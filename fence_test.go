package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// fakeOpenWrt is a minimal SSH server answering the exec commands
// the SWFence handlers run, with canned OpenWrt output.
type fakeOpenWrt struct {
	ln       net.Listener
	failCmds map[string]bool
	extra    map[string]string
}

var fakeWrtOutputs = map[string]string{
	`grep DISTRIB_RELEASE /etc/openwrt_release | cut -d= -f2 | tr -d "'\""`: "24.10.2\n",
	"command -v fw4":                                            "/usr/sbin/fw4\n",
	"command -v swfence":                                        "/usr/bin/swfence\n",
	"uci get swfence.main.enabled":                              "1\n",
	"nft list chain inet fw4 swfence_policy 2>/dev/null":        "table inet fw4 { chain swfence_policy { } }\n",
	"nft list chain inet fw4 swfence_early 2>/dev/null":         "table inet fw4 { chain swfence_early { } }\n",
	"cat /tmp/dhcp.leases 2>/dev/null":                          "1732000000 aa:bb:cc:dd:ee:ff 192.168.1.50 switch2 *\n",
	"ip -4 -o addr show scope global":                           "2: br-lan    inet 127.0.0.1/24 brd 127.0.0.255 scope global br-lan\n",
	"/sbin/sysupgrade -b /tmp/swfence-backup.tar.gz":            "",
	"swfence configure":                                         "{\"ok\": true}\n",
	"swfence status 2>&1; swfence diagnostics 2>&1 | head -40":  "swfence: active\n",
	fenceUploadCmd:                                              "",
	fenceExtractCmd:                                             "",
	fenceFindCmd:                                                "/tmp/swfence-bundle/SWFence/SHA256SUMS\n",
	"cd /tmp/swfence-bundle/SWFence && sha256sum -c SHA256SUMS": "swfence-24.10.ipk: OK\nluc" + "i-app-swfence-24.10.ipk: OK\n",
	"cd /tmp/swfence-bundle/SWFence && opkg install ./swfence-24.10.ipk ./luci-app-swfence-24.10.ipk": "",
	"opkg list-installed":                "libc - 1.0-r1\nkmod-nft-bridge - 6.6.73-r1\n",
	"cat /etc/opkg/distfeeds.conf":       "src/gz openwrt_core https://example.invalid/core\n",
	fenceRepoCmd:                         "",
	fenceOpkgUpdateCmd:                   "",
	fenceApkUpdateCmd:                    "",
	fenceWgetWrapCmd:                     "",
	fenceListFetchCmd:                    "",
	fencePathPrefix + fenceOpkgUpdateCmd: "",
	fencePathPrefix + fenceApkUpdateCmd:  "",
}

func init() {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write([]byte("swfence backup test"))
	gw.Close()
	fakeWrtOutputs["cat /tmp/swfence-backup.tar.gz"] = buf.String()
}

func newFakeOpenWrt(t *testing.T) *fakeOpenWrt {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	conf := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			return nil, nil
		},
	}
	conf.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeOpenWrt{ln: ln, failCmds: map[string]bool{}, extra: map[string]string{}}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.handleConn(c, conf)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeOpenWrt) addr() string { return f.ln.Addr().String() }

func (f *fakeOpenWrt) handleConn(c net.Conn, conf *ssh.ServerConfig) {
	defer c.Close()
	_, chans, reqs, err := ssh.NewServerConn(c, conf)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "session" {
			ch.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		channel, requests, err := ch.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer channel.Close()
			for req := range requests {
				if req.Type != "exec" {
					if req.WantReply {
						req.Reply(false, nil)
					}
					continue
				}
				var payload struct{ Command string }
				ssh.Unmarshal(req.Payload, &payload)
				if req.WantReply {
					req.Reply(true, nil)
				}
				if f.failCmds[payload.Command] {
					channel.Write([]byte("fake error output\n"))
					channel.SendRequest("exit-status", false, []byte{0, 0, 0, 1})
					return
				}
				out, ok := f.extra[payload.Command]
				if !ok {
					out = fakeWrtOutputs[payload.Command]
				}
				channel.Write([]byte(out))
				channel.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
				return
			}
		}()
	}
}

// The page's fenceBody() sends the router address as "host". These
// tests post exactly the page's body shape: they fail if the
// server ever stops reading that key (the address went empty and
// every SWFence operation failed at dial).
func TestFenceChkPageContract(t *testing.T) {
	f := newFakeOpenWrt(t)
	body := `{"host":"` + f.addr() + `","user":"root","pass":"pw","lang":"ja"}`
	req := httptest.NewRequest("POST", "/api/fencechk", strings.NewReader(body))
	rec := httptest.NewRecorder()
	fenceChkHandler(rec, req)
	var j map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &j); err != nil {
		t.Fatalf("response not JSON: %v (%s)", err, rec.Body.String())
	}
	if j["ok"] != true {
		t.Fatalf("chk ok = %v, want true (body: %s)", j["ok"], rec.Body.String())
	}
	if j["supported"] != true || j["installed"] != true || j["enabled"] != true || j["guards"] != true {
		t.Errorf("chk fields = %v", j)
	}
	if j["release"] != "24.10.2" {
		t.Errorf("release = %v", j["release"])
	}
	if j["cidr"] != "127.0.0.0/24" {
		t.Errorf("cidr = %v, want the LAN network of the connected address (the page fills the subnet field from it)", j["cidr"])
	}
	leases, _ := j["leases"].([]any)
	if len(leases) != 1 {
		t.Fatalf("leases = %v", j["leases"])
	}
	l0, _ := leases[0].(map[string]any)
	if l0["ip"] != "192.168.1.50" || l0["mac"] != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("lease = %v", l0)
	}
}

func TestFenceStatusPageContract(t *testing.T) {
	f := newFakeOpenWrt(t)
	body := `{"host":"` + f.addr() + `","user":"root","pass":"pw","lang":"ja"}`
	req := httptest.NewRequest("POST", "/api/fencestatus", strings.NewReader(body))
	rec := httptest.NewRecorder()
	fenceStatusHandler(rec, req)
	var j map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &j); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if j["ok"] != true {
		t.Fatalf("status ok = %v (body: %s)", j["ok"], rec.Body.String())
	}
	if out, _ := j["output"].(string); !strings.Contains(out, "active") {
		t.Errorf("status output = %q", out)
	}
}

func TestFenceChkUnreachable(t *testing.T) {
	body := `{"host":"127.0.0.1:1","user":"root","pass":"pw","lang":"ja"}`
	req := httptest.NewRequest("POST", "/api/fencechk", strings.NewReader(body))
	rec := httptest.NewRecorder()
	fenceChkHandler(rec, req)
	var j map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &j); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if j["ok"] != false || j["error"] == "" {
		t.Errorf("unreachable chk = %v, want ok:false with an error", j)
	}
}

// With a stale KAIHEN_LAN_IP (an address the machine does not own),
// the bound dial fails locally and lanDialTimeout must fall back to an
// unbound dial so the check still reaches the router.
func TestFenceChkStaleLANIPFallback(t *testing.T) {
	addr := newFakeOpenWrt(t).addr()
	os.Setenv("KAIHEN_LAN_IP", "203.0.113.99")
	defer os.Unsetenv("KAIHEN_LAN_IP")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/fencechk",
		strings.NewReader(`{"host":"`+addr+`","user":"root","pass":"pw","lang":"ja"}`))
	fenceChkHandler(rec, req)
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["ok"] != true {
		t.Fatalf("stale KAIHEN_LAN_IP should fall back to an unbound dial: %v", got)
	}
}

func TestFenceInstallBundle(t *testing.T) {
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()
	dir := fenceCacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tag.txt"), []byte("v9.9.9-test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fenceBundleName("v9.9.9-test")), makeTestBundle(), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := fenceCachedTag(); got != "v9.9.9-test" {
		t.Fatalf("fenceCachedTag = %q", got)
	}
	cl, err := fenceDial(newFakeOpenWrt(t).addr(), "pw")
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if err := fenceInstallBundle(cl, "v9.9.9-test", "24.10.2", func(string) {}); err != nil {
		t.Fatal(err)
	}
}

func TestFenceInstallNeedsDownload(t *testing.T) {
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()
	addr := newFakeOpenWrt(t).addr()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/fenceinstall?ip="+addr+"&pass=pw&macw=aa:bb:cc:dd:ee:ff&lang=ja", nil)
	fenceInstallHandler(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "RESULT|ng") || !strings.Contains(body, "ダウンロード") {
		t.Fatalf("install without a downloaded bundle should fail asking for the download first: %s", body)
	}
}

// makeTestIPK builds a minimal but structurally real .ipk whose
// control file declares the given package and dependencies.
func makeTestIPK(pkg, deps string) []byte {
	var ctl bytes.Buffer
	{
		gw := gzip.NewWriter(&ctl)
		tw := tar.NewWriter(gw)
		body := "Package: " + pkg + "\nVersion: 1.0-r1\nDepends: " + deps + "\nArchitecture: all\n"
		tw.WriteHeader(&tar.Header{Name: "./control", Mode: 0o644, Size: int64(len(body))})
		tw.Write([]byte(body))
		tw.Close()
		gw.Close()
	}
	var data bytes.Buffer
	{
		gw := gzip.NewWriter(&data)
		tw := tar.NewWriter(gw)
		tw.Close()
		gw.Close()
	}
	var out bytes.Buffer
	gw := gzip.NewWriter(&out)
	tw := tar.NewWriter(gw)
	for name, b := range map[string][]byte{
		"./debian-binary":  []byte("2.0\n"),
		"./control.tar.gz": ctl.Bytes(),
		"./data.tar.gz":    data.Bytes(),
	} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(b))})
		tw.Write(b)
	}
	tw.Close()
	gw.Close()
	return out.Bytes()
}

// makeTestBundle builds a structurally real SWFence bundle:
// swfence depends on libc + kmod-nft-bridge; the LuCI app on
// libc + swfence.
func makeTestBundle() []byte {
	var out bytes.Buffer
	gw := gzip.NewWriter(&out)
	tw := tar.NewWriter(gw)
	files := map[string][]byte{
		"SWFence/swfence-24.10.ipk":          makeTestIPK("swfence", "libc, kmod-nft-bridge"),
		"SWFence/luci-app-swfence-24.10.ipk": makeTestIPK("luci-app-swfence", "libc, swfence"),
		"SWFence/SHA256SUMS":                 []byte("deadbeef  swfence-24.10.ipk\n"),
	}
	names := []string{"SWFence/SHA256SUMS", "SWFence/luci-app-swfence-24.10.ipk", "SWFence/swfence-24.10.ipk"}
	for _, name := range names {
		b := files[name]
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(b))})
		tw.Write(b)
	}
	tw.Close()
	gw.Close()
	return out.Bytes()
}

func seedFenceCache(t *testing.T) {
	t.Helper()
	dir := fenceCacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tag.txt"), []byte("v9.9.9-test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fenceBundleName("v9.9.9-test")), makeTestBundle(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The whole install handler against the fake router, backup step
// included: this path shipped broken once (it had never been
// exercised end to end), so it stays covered.
func TestFenceInstallHandlerFull(t *testing.T) {
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()
	seedFenceCache(t)
	addr := newFakeOpenWrt(t).addr()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/fenceinstall?ip="+addr+"&pass=pw&macw=aa:bb:cc:dd:ee:ff&subnet=192.168.1.0/24&lang=ja", nil)
	fenceInstallHandler(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "RESULT|ok") {
		t.Fatalf("full install should succeed against the fake router: %s", body)
	}
	ents, err := os.ReadDir(filepath.Join(exeDir, "swfence-backups"))
	if err != nil || len(ents) != 1 || !strings.HasPrefix(ents[0].Name(), "swfence-backup-") {
		t.Fatalf("router backup should be saved locally: ents=%v err=%v", ents, err)
	}
}

func TestFenceInstallBackupFailureDetail(t *testing.T) {
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()
	seedFenceCache(t)
	f := newFakeOpenWrt(t)
	f.failCmds["/sbin/sysupgrade -b /tmp/swfence-backup.tar.gz"] = true
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/fenceinstall?ip="+f.addr()+"&pass=pw&macw=aa:bb:cc:dd:ee:ff&lang=ja", nil)
	fenceInstallHandler(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "RESULT|ng") || !strings.Contains(body, "status 1") {
		t.Fatalf("a failed backup must surface the underlying error, not just the generic message: %s", body)
	}
}

func TestFenceDepParsing(t *testing.T) {
	got := depNamesOf("kernel (= 6.6.73~a21259-r1), kmod-nft-core, a | b, libc")
	want := []string{"kernel", "kmod-nft-core", "a", "libc"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("depNamesOf = %v, want %v", got, want)
	}
	info, err := parseIPKBytes(makeTestIPK("swfence", "libc, kmod-nft-bridge"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Package != "swfence" || strings.Join(info.Depends, ",") != "libc,kmod-nft-bridge" {
		t.Fatalf("parseIPKBytes = %+v", info)
	}
	infos, err := func() ([]ipkInfo, error) {
		old := exeDir
		exeDir = t.TempDir()
		defer func() { exeDir = old }()
		seedFenceCache(t)
		return fenceBundleIPKs("v9.9.9-test")
	}()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("bundle ipks = %d, want 2", len(infos))
	}
}

// The router lacks kmod-nft-bridge and the cache has no dependency
// files: the install must stop and NAME the missing package (this
// is the failure opkg used to report as "Unknown package 'swfence'").
func TestFenceInstallMissingDep(t *testing.T) {
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()
	seedFenceCache(t)
	f := newFakeOpenWrt(t)
	f.extra["opkg list-installed"] = "libc - 1.0-r1\n"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/fenceinstall?ip="+f.addr()+"&pass=pw&macw=aa:bb:cc:dd:ee:ff&lang=ja", nil)
	fenceInstallHandler(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "RESULT|ng") || !strings.Contains(body, "kmod-nft-bridge") || !strings.Contains(body, "不足している依存パッケージがあります") {
		t.Fatalf("a missing dependency should be named in the failure: %s", body)
	}
}

// Same router, but the phone downloaded the dependency beforehand:
// it is uploaded and installed in the same opkg command, offline.
func TestFenceInstallWithCachedDep(t *testing.T) {
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()
	seedFenceCache(t)
	depsDir := filepath.Join(fenceCacheDir(), "deps")
	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	depFile := "kmod-nft-bridge_6.6.73-r1_mipsel_24kc.ipk"
	if err := os.WriteFile(filepath.Join(depsDir, depFile), makeTestIPK("kmod-nft-bridge", "kernel, kmod-nft-core"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := newFakeOpenWrt(t)
	f.extra["opkg list-installed"] = "libc - 1.0-r1\n"
	f.extra["mkdir -p /tmp/swfence-deps"] = ""
	f.extra["cat > /tmp/swfence-deps/"+depFile] = ""
	f.extra["cd /tmp/swfence-bundle/SWFence && opkg install ./swfence-24.10.ipk ./luci-app-swfence-24.10.ipk /tmp/swfence-deps/"+depFile] = ""
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/fenceinstall?ip="+f.addr()+"&pass=pw&macw=aa:bb:cc:dd:ee:ff&lang=ja", nil)
	fenceInstallHandler(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, "RESULT|ok") {
		t.Fatalf("install with the dependency file supplied should succeed: %s", body)
	}
}

// The download step resolves missing packages (and their own
// missing dependencies) against the router's own feed indexes.
func TestFenceDepsDownload(t *testing.T) {
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()
	bridge := "kmod-nft-bridge_6.6.73-r1_mipsel_24kc.ipk"
	core := "kmod-nft-core_6.6.73-r1_mipsel_24kc.ipk"
	index := "Package: kmod-nft-bridge\nVersion: 6.6.73-r1\nDepends: kernel (= 6.6.73~a21259-r1), kmod-nft-core\nFilename: " + bridge + "\n\n" +
		"Package: kmod-nft-core\nVersion: 6.6.73-r1\nDepends: kernel (= 6.6.73~a21259-r1)\nFilename: " + core + "\n\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/core/Packages.gz", func(w http.ResponseWriter, r *http.Request) {
		gw := gzip.NewWriter(w)
		gw.Write([]byte(index))
		gw.Close()
	})
	mux.HandleFunc("/core/"+bridge, func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeTestIPK("kmod-nft-bridge", "kernel, kmod-nft-core"))
	})
	mux.HandleFunc("/core/"+core, func(w http.ResponseWriter, r *http.Request) {
		w.Write(makeTestIPK("kmod-nft-core", "kernel"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	prof := &fenceProfile{
		Feeds:     []fenceFeed{{Name: "openwrt_core", URL: srv.URL + "/core", Gz: true}},
		Installed: []string{"kernel", "libc"},
		Missing:   []string{"kmod-nft-bridge"},
	}
	fetched, unresolved, err := fenceFetchDeps(prof)
	if err != nil {
		t.Fatal(err)
	}
	if len(unresolved) != 0 || len(fetched) != 2 {
		t.Fatalf("fetched = %v, unresolved = %v", fetched, unresolved)
	}
	cached, err := fenceCachedDeps()
	if err != nil {
		t.Fatal(err)
	}
	if cached["kmod-nft-bridge"] == "" || cached["kmod-nft-core"] == "" {
		t.Fatalf("cached deps = %v", cached)
	}
}

// The connection check records the router profile (feeds +
// missing deps) for the download step, and reports the missing
// set to the page.
func TestFenceChkSavesProfile(t *testing.T) {
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()
	seedFenceCache(t)
	f := newFakeOpenWrt(t)
	f.extra["opkg list-installed"] = "libc - 1.0-r1\n"
	body := `{"host":"` + f.addr() + `","user":"root","pass":"pw","lang":"ja"}`
	req := httptest.NewRequest("POST", "/api/fencechk", strings.NewReader(body))
	rec := httptest.NewRecorder()
	fenceChkHandler(rec, req)
	var j map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &j); err != nil {
		t.Fatalf("response not JSON: %v (%s)", err, rec.Body.String())
	}
	miss, _ := j["missing"].([]any)
	if len(miss) != 1 || miss[0] != "kmod-nft-bridge" {
		t.Fatalf("chk missing = %v (body: %s)", j["missing"], rec.Body.String())
	}
	prof := fenceLoadProfile()
	if prof == nil || len(prof.Missing) != 1 || prof.Missing[0] != "kmod-nft-bridge" {
		t.Fatalf("saved profile = %+v", prof)
	}
	if len(prof.Feeds) != 1 || prof.Feeds[0].URL != "https://example.invalid/core" {
		t.Fatalf("profile feeds = %+v", prof.Feeds)
	}
}

func TestFenceValidTag(t *testing.T) {
	for _, ok := range []string{"v1.0.0", "1.2.3", "v0.1.0-beta"} {
		if !fenceValidTag(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, ng := range []string{"", "v1;rm -rf /", "a b", "$(x)", "../x", "-lead"} {
		if fenceValidTag(ng) {
			t.Errorf("%q should be invalid", ng)
		}
	}
}
