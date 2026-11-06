package main

// Read-only version adapter for the ORIGINAL installed XKeen and Xray binaries.
// Only fixed upstream commands are allowed; never run a shell or expose their
// output. A binary version does not prove a working VPN connection.
import (
    "bytes"
    "context"
    "errors"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "regexp"
    "strings"
    "time"
)

type nativeVersionEvidence struct {
    State string `json:"state"`
    Version string `json:"version,omitempty"`
}
type nativeVersionReport struct {
    Source string `json:"source"`
    Runtime string `json:"runtime"`
    XKeen nativeVersionEvidence `json:"xkeen"`
    Xray nativeVersionEvidence `json:"xray"`
}

// This is NOT an arbitrary command runner. It accepts one of two hard-coded
// native commands and only a precise version string is ever published.
var xkeenVersionLine = regexp.MustCompile(`(?im)(?:^|[^a-z0-9])xkeen[^0-9\r\n]{0,44}v?([0-9]{1,4}\.[0-9]{1,4}(?:\.[0-9]{1,4})?)(?:[^0-9.]|$)`)
var xrayVersionLine = regexp.MustCompile(`(?im)(?:^|[^a-z0-9])xray[^0-9\r\n]{0,44}v?([0-9]{1,4}\.[0-9]{1,4}(?:\.[0-9]{1,4})?)(?:[^0-9.]|$)`)
var pureVersionLine = regexp.MustCompile(`(?m)^\s*v?([0-9]{1,4}\.[0-9]{1,4}(?:\.[0-9]{1,4})?)\s*$`)

func parseNativeVersion(kind,stringOutput string) string {
    var re *regexp.Regexp
    switch kind {
    case "xkeen": re=xkeenVersionLine
    case "xray": re=xrayVersionLine
    default: return ""
    }
    value:=re.FindStringSubmatch(stringOutput)
    if len(value)>1{return value[1]}
    // Some official -v scripts output exactly "2.1" with no product name.
    if simple:=pureVersionLine.FindStringSubmatch(strings.TrimSpace(stringOutput));len(simple)>1 {
        return simple[1]
    }
    return ""
}

type cappedStdout struct {
    buf bytes.Buffer
    max int
}
func (w *cappedStdout) Write(p []byte)(int,error){
    if len(p)>w.max-w.buf.Len(){return 0,errors.New("native version stdout limit")}
    return w.buf.Write(p)
}

func oneNativeVersion(ctx context.Context,root,kind string) nativeVersionEvidence {
    var binary,arg string
    switch kind {
    case "xkeen": binary,arg="xkeen","-v"
    case "xray": binary,arg="xray","version"
    default: return nativeVersionEvidence{State:"UNKNOWN"}
    }
    state:=observedPath(root,filepath.Join("sbin",binary),false)
    if state!="PRESENT" {return nativeVersionEvidence{State:state}}
    path:=filepath.Join(root,"sbin",binary)
    info,err:=os.Lstat(path)
    if err!=nil || !info.Mode().IsRegular() || info.Mode()&0111==0 {
        return nativeVersionEvidence{State:"UNKNOWN"}
    }
    callCtx,cancel:=context.WithTimeout(ctx,3*time.Second)
    defer cancel()
    stdout:=&cappedStdout{max:8192}
    cmd:=exec.CommandContext(callCtx,path,arg)
    cmd.Stdout=stdout
    cmd.Stderr=io.Discard
    // A detached child must not hold Go's copied stdout pipe indefinitely.
    cmd.WaitDelay=250*time.Millisecond
    if err:=cmd.Run();err!=nil {
        if errors.Is(callCtx.Err(),context.DeadlineExceeded){
            return nativeVersionEvidence{State:"TIMEOUT"}
        }
        return nativeVersionEvidence{State:"UNKNOWN"}
    }
    version:=parseNativeVersion(kind,stdout.buf.String())
    if version==""{return nativeVersionEvidence{State:"UNKNOWN"}}
    return nativeVersionEvidence{State:"VERSION_DETECTED",Version:version}
}

func detectNativeVersions(ctx context.Context,root string) nativeVersionReport {
    return nativeVersionReport{
        Source:"OFFICIAL_CLI_READ_ONLY",Runtime:"NOT_VERIFIED",
        XKeen:oneNativeVersion(ctx,root,"xkeen"),
        Xray:oneNativeVersion(ctx,root,"xray"),
    }
}
