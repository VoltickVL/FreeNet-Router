package main

// XKeen 2.2 Beta owns its speed-balancer and Xray API. This adapter only
// dispatches a fixed, documented CLI action; it never parses CLI output.
import (
 "context"
 "errors"
 "io"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "time"
)

type nativeBalancerActionReport struct {
 State string `json:"state"`
 Runtime string `json:"runtime"`
 Action string `json:"action,omitempty"`
}

func nativeBalancerAction(ctx context.Context, root, procRoot, action string) nativeBalancerActionReport {
 result:=nativeBalancerActionReport{State:"STOP",Runtime:"NOT_VERIFIED",Action:action}
 if action!="next" && action!="prev" && action!="auto" {return result}
 // An installed 0.8 init can own network policy: do not become a second writer.
 if observedPath(root,"etc/init.d/S99freenet-ui",false)!="MISSING" {return result}
 if observedPath(root,"sbin/xkeen",false)!="PRESENT" {return result}
 if observedPath(root,"etc/xray/configs",true)!="PRESENT" {return result}
 // 2.1 semantics must not be silently carried forward to an unknown upstream.
 version:=oneNativeVersion(ctx,root,"xkeen")
 if version.State!="VERSION_DETECTED" || version.Version!="2.2" {return result}
 // This is evidence of the service/process, NOT evidence of working VPN egress.
 status:=nativeXKeenServiceStatus(ctx,root,procRoot)
 if status.State!="RUNNING_PROCESS_DETECTED" {return result}
 path:=filepath.Join(root,"sbin/xkeen")
 info,err:=os.Lstat(path)
 if err!=nil || !info.Mode().IsRegular() || info.Mode()&0111==0 {return result}
 // Only the official non-interactive subcommands. No user-provided tags or
 // node indices, no shell invocation, no upstream stdout/stderr or credentials.
 deadline:=20*time.Second
 if action=="auto" {deadline=45*time.Second} // upstream may run a speed probe
 callCtx,cancel:=context.WithTimeout(ctx,deadline)
 defer cancel()
 cmd:=exec.CommandContext(callCtx,path,"-sb",action)
 cmd.Stdout=io.Discard
 cmd.Stderr=io.Discard
 cmd.Stdin=strings.NewReader("")
 cmd.WaitDelay=500*time.Millisecond
 if err=cmd.Run();err!=nil {
  if errors.Is(callCtx.Err(),context.DeadlineExceeded){result.State="TIMEOUT"} else {result.State="UNKNOWN"}
  return result
 }
 if callCtx.Err()!=nil {result.State="UNKNOWN";return result}
 // XKeen's exit=0 does NOT prove the selected tag or clear the override:
 // 2.2 sb_auto can return 0 when its API was unavailable. Never say APPLIED.
 result.State="UPSTREAM_COMMAND_ACCEPTED_NOT_VERIFIED"
 return result
}
