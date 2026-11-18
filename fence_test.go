package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// fakeOpenWrt is a minimal SSH server answering the exec commands
// the SWFence handlers run, with canned OpenWrt output.
type fakeOpenWrt struct {
	ln net.Listener
}

var fakeWrtOutputs = map[string]string{
	`grep DISTRIB_RELEASE /etc/openwrt_release | cut -d= -f2 | tr -d "'\""`: "24.10.2\n",
	"command -v fw4":     "/usr/sbin/fw4\n",
	"command -v swfence": "/usr/bin/swfence\n",
	"uci get swfence.main.enabled":                         "1\n",
	"nft list chain inet fw4 swfence_policy 2>/dev/null":   "table inet fw4 { chain swfence_policy { } }\n",
	"nft list chain inet fw4 swfence_early 2>/dev/null":    "table inet fw4 { chain swfence_early { } }\n",
	"cat /tmp/dhcp.leases 2>/dev/null":                     "1732000000 aa:bb:cc:dd:ee:ff 192.168.1.50 switch2 *\n",
	"swfence status 2>&1; swfence diagnostics 2>&1 | head -40": "swfence: active\n",
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
	f := &fakeOpenWrt{ln: ln}
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
				channel.Write([]byte(fakeWrtOutputs[payload.Command]))
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
