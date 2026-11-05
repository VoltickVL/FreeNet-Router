package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	visionQUICStandard = "xtls-rprx-vision"
	visionQUICAllowed = "xtls-rprx-vision-udp443"
	visionQUICMaxConfigBytes = 256 * 1024
)

// rejectDuplicateJSONKeys keeps unknown/contradictory JSON from being silently
// normalized during a transport experiment. Never report raw config in errors.
func rejectDuplicateJSONKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		tok, err := d.Token()
		if err != nil { return errors.New("invalid JSON") }
		delim, ok := tok.(json.Delim)
		if !ok { return nil }
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				keyTok, err := d.Token()
				if err != nil { return errors.New("invalid JSON") }
				key, ok := keyTok.(string)
				if !ok || seen[key] { return errors.New("ambiguous JSON") }
				seen[key] = true
				if err := walk(); err != nil { return err }
			}
		case '[':
			for d.More() {
				if err := walk(); err != nil { return err }
			}
		default:
			return errors.New("invalid JSON")
		}
		end, err := d.Token()
		if err != nil {return errors.New("invalid JSON")}
		switch delim {
		case '{':
			if end != json.Delim('}') {return errors.New("invalid JSON")}
		case '[':
			if end != json.Delim(']') {return errors.New("invalid JSON")}
		default:
			return errors.New("invalid JSON")
		}
		return nil
	}
	if len(data) == 0 || len(data) > visionQUICMaxConfigBytes {
		return errors.New("outbound size invalid")
	}
	if err := walk(); err != nil { return err }
	if _, err := d.Token(); err != io.EOF { return errors.New("trailing JSON") }
	return nil
}

func visionQUICPlanToken(data []byte, target string) string {
	sum := sha256.Sum256(append(append([]byte("freenet-vision-quic-v1:"), []byte(target)...), data...))
	return hex.EncodeToString(sum[:])
}

func visionQUICMode(target string) bool {
	return target == visionQUICStandard || target == visionQUICAllowed
}

func visionQUICCandidate(data []byte, target string) ([]byte, string, error) {
	if !visionQUICMode(target) { return nil, "", errors.New("unsupported transport mode") }
	if err := rejectDuplicateJSONKeys(data); err != nil { return nil, "", err }
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil { return nil, "", errors.New("outbound is invalid") }
	var outbounds []json.RawMessage
	if err := json.Unmarshal(root["outbounds"], &outbounds); err != nil || len(outbounds) == 0 {
		return nil, "", errors.New("outbound list unavailable")
	}
	index := -1
	var active map[string]json.RawMessage
	for i, raw := range outbounds {
		var current map[string]json.RawMessage
		if json.Unmarshal(raw, &current) != nil { return nil, "", errors.New("outbound structure invalid") }
		var tag string
		if json.Unmarshal(current["tag"], &tag) != nil { return nil, "", errors.New("outbound tag invalid") }
		if tag == "vless-reality" {
			if index != -1 { return nil, "", errors.New("multiple active VPN outbounds") }
			index, active = i, current
		}
	}
	if index == -1 { return nil, "", errors.New("active VLESS outbound unavailable") }
	var protocol string
	if json.Unmarshal(active["protocol"], &protocol) != nil || protocol != "vless" {
		return nil, "", errors.New("unsupported VPN protocol")
	}
	var stream map[string]json.RawMessage
	if json.Unmarshal(active["streamSettings"], &stream) != nil {
		return nil, "", errors.New("stream settings unavailable")
	}
	var network, security string
	_ = json.Unmarshal(stream["network"], &network)
	_ = json.Unmarshal(stream["security"], &security)
	if security != "reality" || (network != "tcp" && network != "raw") {
		return nil, "", errors.New("only VLESS Reality over TCP is supported")
	}
	var settings map[string]json.RawMessage
	if json.Unmarshal(active["settings"], &settings) != nil {
		return nil, "", errors.New("VLESS settings unavailable")
	}
	var vnext []map[string]json.RawMessage
	if json.Unmarshal(settings["vnext"], &vnext) != nil || len(vnext) != 1 {
		return nil, "", errors.New("VLESS destination ambiguous")
	}
	var users []map[string]json.RawMessage
	if json.Unmarshal(vnext[0]["users"], &users) != nil || len(users) != 1 {
		return nil, "", errors.New("VLESS identity ambiguous")
	}
	var original string
	if json.Unmarshal(users[0]["flow"], &original) != nil || !visionQUICMode(original) {
		return nil, "", errors.New("current Vision mode unsupported")
	}
	if original == target { return nil, original, errors.New("transport already configured") }
	if len(users[0]["id"]) == 0 || len(vnext[0]["address"]) == 0 || len(vnext[0]["port"]) == 0 {
		return nil, "", errors.New("active profile incomplete")
	}
	encodedFlow, _ := json.Marshal(target)
	users[0]["flow"] = encodedFlow
	encodedUsers, _ := json.Marshal(users)
	vnext[0]["users"] = encodedUsers
	encodedDestinations, _ := json.Marshal(vnext)
	settings["vnext"] = encodedDestinations
	encodedSettings, _ := json.Marshal(settings)
	active["settings"] = encodedSettings
	encodedActive, _ := json.Marshal(active)
	outbounds[index] = encodedActive
	encodedOutbounds, _ := json.Marshal(outbounds)
	root["outbounds"] = encodedOutbounds
	result, err := json.Marshal(root)
	if err != nil || len(result) > visionQUICMaxConfigBytes {
		return nil, "", errors.New("candidate encoding failed")
	}
	return result, original, nil
}

// The Xray runtime CLI cannot reliably validate a streamed /dev/stdin config
// on Entware. Stage the exact candidate in a private 0600 temporary file and
// discard all Xray output (which could include credential-bearing values).
func visionQUICPreflightWith(ctx context.Context, candidate []byte, run func(context.Context, string) error) error {
	if ctx == nil || run == nil || len(candidate) == 0 || len(candidate) > visionQUICMaxConfigBytes {
		return errors.New("candidate preflight unavailable")
	}
	dir, err := os.MkdirTemp("", "freenet-vision-candidate-")
	if err != nil { return errors.New("private candidate directory unavailable") }
	defer os.RemoveAll(dir)
	_ = os.Chmod(dir, 0700)
	path := filepath.Join(dir, "candidate.json")
	if os.WriteFile(path, candidate, 0600) != nil { return errors.New("candidate staging failed") }
	if ctx.Err() != nil { return errors.New("candidate preflight cancelled") }
	if run(ctx, path) != nil || ctx.Err() != nil {
		return errors.New("candidate Xray validation failed")
	}
	return nil
}

// Run a whole-config Xray semantic test against a private copy of the
// installed config directory. Never validate only the outbound fragment or
// mutate production configuration during preflight.
func visionQUICPreflight(ctx context.Context, binary, assetDir, liveDir, outboundName string, candidate []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	if strings.TrimSpace(binary)=="" || strings.TrimSpace(liveDir)=="" || outboundName!="04_outbounds.json" ||
		len(candidate)==0 || len(candidate)>visionQUICMaxConfigBytes {
		return errors.New("candidate Xray preflight unavailable")
	}
	entries, err := os.ReadDir(liveDir)
	if err!=nil || len(entries)>64 {return errors.New("live config directory unavailable")}
	dir, err:=os.MkdirTemp("", "freenet-vision-confdir-")
	if err!=nil {return errors.New("private config directory unavailable")}
	defer os.RemoveAll(dir)
	_ = os.Chmod(dir,0700)
	found:=false
	for _,entry:=range entries{
		name:=entry.Name()
		if !strings.HasSuffix(name,".json") && !strings.HasSuffix(name,".jsonc"){
			continue
		}
		// A symlink/directory disguised as a config fragment could make the
		// isolated test pass against a *different* effective live config.
		if entry.IsDir() || entry.Type()&os.ModeSymlink!=0 {
			return errors.New("live config fragment is not a regular file")
		}
		if name=="04_outbounds.json"{found=true}
		raw:=candidate
		if name!="04_outbounds.json"{
			info,e:=entry.Info()
			if e!=nil || !info.Mode().IsRegular() || info.Size()<=0 || info.Size()>1024*1024 {
				return errors.New("live config fragment invalid")
			}
			raw,e=os.ReadFile(filepath.Join(liveDir,name))
			if e!=nil {return errors.New("live config fragment unavailable")}
		}
		if os.WriteFile(filepath.Join(dir,name),raw,0600)!=nil{
			return errors.New("private configuration staging failed")
		}
	}
	if !found || ctx.Err()!=nil {return errors.New("candidate Xray preflight incomplete")}
	cmd:=exec.CommandContext(ctx,binary,"run","-test","-confdir",dir)
	cmd.Env=isolatedXrayProbeEnv(append(os.Environ(),"XRAY_LOCATION_ASSET="+assetDir))
	cmd.Stdout=io.Discard
	cmd.Stderr=io.Discard
	cmd.WaitDelay=2*time.Second
	if cmd.Run()!=nil || ctx.Err()!=nil {return errors.New("candidate Xray preflight failed")}
	return nil
}
