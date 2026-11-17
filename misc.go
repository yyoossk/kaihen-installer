package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func pageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	io.WriteString(w, strings.ReplaceAll(pageHTML, "__BUILDSTAMP__", buildStamp))
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func pingHost(ip string) bool {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("ping", "-n", "1", "-w", "2000", ip)
	} else {
		cmd = exec.Command("ping", "-c", "1", "-W", "2", ip)
	}
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "ttl=")
}

func checkHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IP   string `json:"ip"`
		Lang string `json:"lang"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if !validIP(body.IP) {
		jsonOK(w, map[string]any{"ok": false, "conn": false, "ping": false, "detail": tr(body.Lang, "ipInvalid")})
		return
	}
	ok, why := probeLoader(body.IP)
	detail := ""
	if !ok {
		switch {
		case strings.HasPrefix(why, "err:"):
			detail = tr(body.Lang, "chkDetailErr", strings.TrimPrefix(why, "err:"))
		case why == "noreply":
			detail = tr(body.Lang, "chkDetailNone")
		case strings.HasPrefix(why, "mismatch:"):
			detail = tr(body.Lang, "chkDetailDiff", strings.TrimPrefix(why, "mismatch:"))
		}
	}
	jsonOK(w, map[string]any{"ok": ok, "conn": ok, "ping": pingHost(body.IP), "detail": detail})
}

func quitHandler(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{"ok": true})
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	}()
}

func openFolderHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	var dir string
	switch body.Name {
	case "pico":
		dir = pathOf("pico")
	case "pico-jp":
		dir = pathOf("pico", "jp")
	case "pico-en":
		dir = pathOf("pico", "en")
	case "autoloader-sd":
		dir = pathOf("autoloader-sd")
	default:
		http.Error(w, "bad folder", http.StatusBadRequest)
		return
	}
	os.MkdirAll(dir, 0o755)
	mirrorFolderExternal(dir)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	cmd.Start()
	jsonOK(w, map[string]any{"ok": true})
}

// --- ipconfig ---------------------------------------------------------------

func ipconfigText() (string, error) {
	if f := os.Getenv("CIVINSTALLER_IPCONFIG_FILE"); f != "" {
		b, err := os.ReadFile(f)
		return string(b), err
	}
	if runtime.GOOS != "windows" {
		return "", errors.New("windows only")
	}
	out, err := exec.Command("cmd", "/c", "chcp 65001 >nul & ipconfig /all").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseIPConfig(text string) (gw, cidr, ip, mask string) {
	var curIP, curMask, curGW string
	flush := func() (string, string, string, string, bool) {
		if curIP != "" && curGW != "" && curMask != "" {
			return curGW, cidrOf(curIP, curMask), curIP, curMask, true
		}
		return "", "", "", "", false
	}
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		low := strings.ToLower(t)
		isField := strings.Contains(t, ":")
		if !isField {
			if g, c, i2, m, ok := flush(); ok {
				return g, c, i2, m
			}
			curIP, curMask, curGW = "", "", ""
			continue
		}
		val := strings.TrimSpace(t[strings.Index(t, ":")+1:])
		val = strings.TrimSpace(strings.TrimSuffix(val, "(優先)"))
		val = strings.TrimSpace(strings.TrimSuffix(val, "(Preferred)"))
		switch {
		case strings.Contains(t, "IPv4") || strings.Contains(low, "ipv4 address"):
			if ip2 := net.ParseIP(val); ip2 != nil && curIP == "" {
				curIP = val
			}
		case strings.Contains(t, "マスク") || strings.Contains(t, "Subnet Mask") || strings.Contains(low, "subnet mask"):
			if net.ParseIP(val) != nil {
				curMask = val
			}
		case strings.Contains(t, "ゲートウェイ") || strings.Contains(low, "default gateway"):
			if net.ParseIP(val) != nil && curGW == "" {
				curGW = val
			}
		}
	}
	if g, c, i2, m, ok := flush(); ok {
		return g, c, i2, m
	}
	return "", "", "", ""
}

func cidrOf(ip, mask string) string {
	ip4 := net.ParseIP(ip).To4()
	m4 := net.ParseIP(mask).To4()
	if ip4 == nil || m4 == nil {
		return ""
	}
	ones, _ := net.IPMask(m4).Size()
	netIP := ip4.Mask(net.IPMask(m4))
	return fmt.Sprintf("%s/%d", netIP.String(), ones)
}

func ipconfigHandler(w http.ResponseWriter, r *http.Request) {
	text, err := ipconfigText()
	if err != nil {
		jsonOK(w, map[string]any{"error": "ipconfig failed"})
		return
	}
	gw, cidr, ip, mask := parseIPConfig(text)
	jsonOK(w, map[string]any{"gateway": gw, "cidr": cidr, "ip": ip, "mask": mask})
}

// --- updates ----------------------------------------------------------------

func httpGet(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "CivInstaller")
	cl := &http.Client{Timeout: 120 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 512<<20))
}

// httpGetFinal is httpGet plus the final URL after redirects (GitHub
// release pages encode the tag in the redirect target).
func httpGetFinal(u string) ([]byte, string, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "CivInstaller")
	cl := &http.Client{Timeout: 120 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, "", err
	}
	return b, resp.Request.URL.String(), nil
}

func getJSON(url string, v any) error {
	b, err := httpGet(url)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// ftpUpload uploads one file to the CivHomebrew FTP server on ip:5000.

type ftpReader struct {
	conn net.Conn
	buf  []byte
}

func newFTPReader(c net.Conn) *ftpReader { return &ftpReader{conn: c} }

func (f *ftpReader) readLine() (string, error) {
	for {
		if i := bytes.IndexByte(f.buf, '\n'); i >= 0 {
			line := string(f.buf[:i+1])
			f.buf = f.buf[i+1:]
			return strings.TrimRight(line, "\r\n"), nil
		}
		tmp := make([]byte, 4096)
		n, err := f.conn.Read(tmp)
		if n > 0 {
			f.buf = append(f.buf, tmp[:n]...)
		}
		if err != nil {
			return "", err
		}
	}
}

func (f *ftpReader) readReply() (int, string) {
	line, err := f.readLine()
	if err != nil {
		return 0, ""
	}
	code := 0
	fmt.Sscanf(line, "%d", &code)
	if len(line) > 3 && line[3] == '-' {
		// multiline: read until "NNN " line
		for {
			l2, err := f.readLine()
			if err != nil {
				break
			}
			if len(l2) > 3 {
				c2 := 0
				fmt.Sscanf(l2, "%d", &c2)
				if c2 == code && l2[3] == ' ' {
					break
				}
			}
		}
	}
	return code, line
}

// ---------- startup consent gate ----------

func consentGiven() bool {
	_, err := os.Stat(pathOf("consent-agreed.txt"))
	return err == nil
}

func consentStateHandler(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{"given": consentGiven()})
}

func consentAgreeHandler(w http.ResponseWriter, r *http.Request) {
	_ = os.WriteFile(pathOf("consent-agreed.txt"), []byte(time.Now().Format(time.RFC3339)), 0o644)
	jsonOK(w, map[string]any{"ok": true})
}
