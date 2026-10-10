package main

import (
    "io"
    "net/http"
    "os"
    "path/filepath"
    "runtime"
    "strconv"
    "strings"
    "syscall"
)

const connectorHostMeminfoLimit = 64 << 10

// linuxStatfsNoexec is the Linux ST_NOEXEC flag (not a guessed mount option).
// These values are informational. Only an actual candidate invocation can
// confirm that tunnel-client runs on the device.
const linuxStatfsNoexec = 0x8

type connectorHostPreflight struct {
    Success bool `json:"success"`
    Mutation string `json:"mutation"`
    OS string `json:"router_os"`
    Arch string `json:"router_arch"`
    LocalMCP string `json:"local_mcp"`
    Storage string `json:"storage"`
    FreeBytes uint64 `json:"free_bytes,omitempty"`
    StorageExec string `json:"storage_exec"`
    MemoryAvailableBytes uint64 `json:"memory_available_bytes,omitempty"`
    MemoryEvidence string `json:"memory_evidence"`
    Candidate string `json:"candidate"`
    Preconditions string `json:"preconditions"`
    ExecutableVerified bool `json:"executable_verified"`
    OpenAIConnected bool `json:"openai_connected"`
    ChatGPTConnected bool `json:"chatgpt_connected"`
    NetworkEvidence string `json:"network_evidence"`
    Note string `json:"note"`
}

func connectorAvailableMemory(input []byte) (uint64, bool) {
    if len(input) == 0 || len(input) > connectorHostMeminfoLimit { return 0, false }
    var found bool
    var value uint64
    for _, line := range strings.Split(string(input), "\n") {
        fields := strings.Fields(line)
        if len(fields) == 0 || fields[0] != "MemAvailable:" { continue }
        if found || len(fields) != 3 || fields[2] != "kB" { return 0, false }
        n, err := strconv.ParseUint(fields[1], 10, 64)
        if err != nil || n == 0 || n > ^uint64(0)/1024 { return 0, false }
        value, found = n*1024, true
    }
    return value, found
}

func connectorHostPreflightFor(goos, arch, listener, destination string, meminfo []byte) connectorHostPreflight {
    p := connectorHostPreflight{
        Success: true, Mutation: "NONE", OS: goos, Arch: arch,
        LocalMCP: "NOT_VERIFIED", Storage: "UNKNOWN", StorageExec: "UNKNOWN",
        MemoryEvidence: "UNKNOWN", Candidate: "UNKNOWN", Preconditions: "STOP",
        ExecutableVerified: false, OpenAIConnected: false, ChatGPTConnected: false,
        NetworkEvidence: "NOT_TESTED",
        Note: "Проверены только локальные предпосылки. Совместимость ELF/ABI, фактический запуск, HTTPS и ChatGPT НЕ проверялись.",
    }
    if conn := connectorReadinessFor(goos, arch, listener); conn.MCPReadyOnRouter {
        p.LocalMCP = "CONFIG_ONLY"
    }
    if goos != "linux" || (arch != "arm64" && arch != "amd64") {
        p.Note = "STOP: официальная сборка для архитектуры не подтверждена."
        return p
    }
    if !filepath.IsAbs(destination) || destination == "/" {return p}
    parent := filepath.Dir(destination)
    dir, err := os.Stat(parent)
    if err != nil || !dir.IsDir() { return p }
    var fs syscall.Statfs_t
    if err := syscall.Statfs(parent, &fs); err != nil || fs.Bsize <= 0 {return p}
    p.Storage = "OBSERVED"
    p.FreeBytes = fs.Bavail * uint64(fs.Bsize)
    if uint64(fs.Flags)&linuxStatfsNoexec != 0 {
        p.StorageExec = "NOEXEC"
    } else {
        p.StorageExec = "EXEC_FLAG_ALLOWED"
    }
    candidate, err := os.Lstat(destination)
    switch {
    case os.IsNotExist(err):
        p.Candidate = "ABSENT"
    case err != nil:
        p.Candidate = "UNKNOWN"
    case candidate.Mode().IsRegular():
        p.Candidate = "EXISTING_UNVERIFIED"
    default:
        p.Candidate = "UNSAFE_FILE_TYPE"
    }
    if bytes, ok := connectorAvailableMemory(meminfo); ok {
        p.MemoryEvidence = "PROC_MEMINFO"
        p.MemoryAvailableBytes = bytes
    }
    if p.LocalMCP == "CONFIG_ONLY" && p.Storage == "OBSERVED" && p.StorageExec == "EXEC_FLAG_ALLOWED" &&
        p.FreeBytes >= tunnelMinFree && p.MemoryEvidence == "PROC_MEMINFO" && p.Candidate == "ABSENT" {
        p.Preconditions = "HOST_FACTS_OBSERVED"
        p.Note = "Базовые ресурсы обнаружены. Порог безопасного расхода RAM не установлен: требуется ограниченная проверка кандидата; tunnel-client и ChatGPT НЕ подключены."
    }
    return p
}

func (a *app) handleConnectorHostPreflight(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Cache-Control", "no-store")
    if len(r.URL.Query()) != 0 {
        writeJSON(w, http.StatusBadRequest, map[string]any{"success":false,"mutation":"NONE","error":"Параметры запрещены"})
        return
    }
    var meminfo []byte
    if f, err := os.Open("/proc/meminfo"); err == nil {
        meminfo, _ = io.ReadAll(io.LimitReader(f, connectorHostMeminfoLimit+1))
        _ = f.Close()
    }
    result := connectorHostPreflightFor(runtime.GOOS, runtime.GOARCH, a.cfg.Listen, tunnelInstallPath(), meminfo)
    writeJSON(w, http.StatusOK, result)
}
