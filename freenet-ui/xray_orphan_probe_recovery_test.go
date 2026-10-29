package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func orphanProbeFixture(t *testing.T) (root, binary, work string, pid int) {
	t.Helper()
	root = t.TempDir()
	binary = filepath.Join(t.TempDir(), "xray")
	if err := os.WriteFile(binary, []byte("test-xray-binary"), 0700); err != nil {
		t.Fatal(err)
	}
	var err error
	work, err = os.MkdirTemp("", "freenet-orphan-reap-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	if err := os.WriteFile(filepath.Join(work, "00_probe.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	pid = 16723
	proc := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(proc, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, bytes []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(proc, name), bytes, 0600); err != nil {
			t.Fatal(err)
		}
	}
	uid := strconv.Itoa(os.Geteuid())
	write("comm", []byte("fn-xray-probe\n"))
	write("status", []byte(fmt.Sprintf("Name:\tfn-xray-probe\nState:\tS (sleeping)\nPPid:\t1\nUid:\t%s\t%s\t%s\t%s\n",
		uid, uid, uid, uid)))
	write("environ", []byte(isolatedXrayProbeFlag+"\x00XRAY_LOCATION_ASSET=/tmp/fake\x00"))
	write("cmdline", []byte(filepath.Join(work, isolatedXrayProbeName)+"\x00run\x00-confdir\x00"+work+"\x00"))
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0], fields[1], fields[19] = "S", "1", "880000"
	write("stat", []byte(fmt.Sprintf("%d (fn-xray-probe) %s\n", pid, strings.Join(fields, " "))))
	if err := os.Symlink(binary, filepath.Join(proc, "exe")); err != nil {
		t.Fatal(err)
	}
	return root, binary, work, pid
}

func TestOrphanedFreeNetProbeReapIsPinnedAndNeverTouchesProduction(t *testing.T) {
	root, binary, _, pid := orphanProbeFixture(t)
	production := filepath.Join(root, "16724")
	if err := os.Mkdir(production, 0700); err != nil {t.Fatal(err)}
	if err := os.WriteFile(filepath.Join(production, "comm"), []byte("xray\n"), 0600); err != nil {t.Fatal(err)}
	signalled := []int{}
	err := reapVerifiedOrphanedXrayProbesWith(context.Background(), root, binary, func(got int) error {
		signalled = append(signalled, got)
		return os.RemoveAll(filepath.Join(root, strconv.Itoa(got)))
	})
	if err != nil || len(signalled) != 1 || signalled[0] != pid {
		t.Fatalf("only trusted abandoned FreeNet child may be reaped: err=%v signalled=%v", err, signalled)
	}
	if _, err := os.Stat(production); err != nil {
		t.Fatalf("production Xray process was touched: %v", err)
	}
}

func TestOrphanedFreeNetProbeCannotRetireOwnedOrUnverifiedProcess(t *testing.T) {
	cases := []struct {
		name string
		alter func(t *testing.T, root, binary, work string, pid int)
		wantErr bool
	}{
		{"live parent is not an orphan",func(t *testing.T, root,binary,work string,pid int){
			p:=filepath.Join(root,strconv.Itoa(pid),"status")
			uid:=strconv.Itoa(os.Geteuid())
			os.WriteFile(p,[]byte(fmt.Sprintf("State:\tS (sleeping)\nPPid:\t987\nUid:\t%s\t%s\t%s\t%s\n",uid,uid,uid,uid)),0600)
		},false},
		{"no FreeNet probe marker",func(t *testing.T,root,binary,work string,pid int){
			os.WriteFile(filepath.Join(root,strconv.Itoa(pid),"environ"),[]byte("OTHER=1\x00"),0600)
		},false},
		{"non-private config directory",func(t *testing.T,root,binary,work string,pid int){
			os.Chmod(work,0755)
		},true},
		{"missing probe config",func(t *testing.T,root,binary,work string,pid int){
			os.Remove(filepath.Join(work,"00_probe.json"))
		},true},
		{"wrong executable inode",func(t *testing.T,root,binary,work string,pid int){
			exe:=filepath.Join(root,strconv.Itoa(pid),"exe")
			os.Remove(exe)
			other:=filepath.Join(t.TempDir(),"other-xray")
			os.WriteFile(other,[]byte("different"),0700)
			os.Symlink(other,exe)
		},true},
		{"foreign argv0",func(t *testing.T,root,binary,work string,pid int){
			os.WriteFile(filepath.Join(root,strconv.Itoa(pid),"cmdline"),[]byte("/usr/bin/xray\x00run\x00-confdir\x00"+work+"\x00"),0600)
		},true},
		{"non-owner UID",func(t *testing.T,root,binary,work string,pid int){
			os.WriteFile(filepath.Join(root,strconv.Itoa(pid),"status"),[]byte("State:\tS\nPPid:\t1\nUid:\t99999\t99999\t99999\t99999\n"),0600)
		},true},
		{"zombie is not a signal target",func(t *testing.T,root,binary,work string,pid int){
			os.WriteFile(filepath.Join(root,strconv.Itoa(pid),"status"),[]byte(fmt.Sprintf("State:\tZ\nPPid:\t1\nUid:\t%d\n",os.Geteuid())),0600)
		},true},
	}
	for _, tc := range cases {
		t.Run(tc.name,func(t *testing.T){
			root,binary,work,pid:=orphanProbeFixture(t)
			tc.alter(t,root,binary,work,pid)
			calls:=0
			err:=reapVerifiedOrphanedXrayProbesWith(context.Background(),root,binary,func(int)error{calls++;return nil})
			if calls!=0 || (err!=nil)!=tc.wantErr {
				t.Fatalf("unsafe probe ownership changed: calls=%d err=%v wantErr=%v",calls,err,tc.wantErr)
			}
		})
	}
}

func TestOrphanedFreeNetProbeCannotClaimInterruptedOrFailedReap(t *testing.T) {
	t.Run("signal denied",func(t *testing.T){
		root,binary,_,_:=orphanProbeFixture(t)
		if err:=reapVerifiedOrphanedXrayProbesWith(context.Background(),root,binary,func(int)error{return syscall.EPERM});err==nil {
			t.Fatal("unconfirmed signal was treated as successful cleanup")
		}
	})
	t.Run("zombie after signal",func(t *testing.T){
		root,binary,_,pid:=orphanProbeFixture(t)
		now:=time.Now()
		err:=reapVerifiedOrphanedXrayProbesWith(context.Background(),root,binary,func(int)error{
			stat:=filepath.Join(root,strconv.Itoa(pid),"stat")
			content,_:=os.ReadFile(stat)
			os.WriteFile(stat,[]byte(strings.Replace(string(content),") S ",") Z ",1)),0600)
			return nil
		})
		if err==nil || time.Since(now)>time.Second {
			t.Fatalf("zombie must STOP immediately; elapsed=%s err=%v",time.Since(now),err)
		}
	})
	t.Run("PID reused after signal",func(t *testing.T){
		root,binary,_,pid:=orphanProbeFixture(t)
		err:=reapVerifiedOrphanedXrayProbesWith(context.Background(),root,binary,func(int)error{
			stat:=filepath.Join(root,strconv.Itoa(pid),"stat")
			content,_:=os.ReadFile(stat)
			os.WriteFile(stat,[]byte(strings.Replace(string(content),"880000","880001",1)),0600)
			return nil
		})
		if err==nil {t.Fatal("PID reuse must STOP before next mutation")}
	})
	t.Run("canceled",func(t *testing.T){
		root,binary,_,_:=orphanProbeFixture(t)
		ctx,cancel:=context.WithCancel(context.Background())
		cancel()
		if err:=reapVerifiedOrphanedXrayProbesWith(ctx,root,binary,func(int)error{
			t.Fatal("canceled request must not signal")
			return nil
		});!errors.Is(err,errOrphanedXrayProbeUnconfirmed){
			t.Fatalf("cancellation was ignored: %v",err)
		}
	})
}
