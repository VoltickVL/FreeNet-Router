/* Native stop-only fixture. All sockets/files live in a private CI case.
 * Deliberately exit the leader thread while a sibling retains the listeners:
 * this reproduces a real Linux lifecycle state that server.Close() hides.
 */
#define _GNU_SOURCE
#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <pthread.h>
#include <signal.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/syscall.h>
#include <time.h>
#include <unistd.h>

#if ATOMIC_INT_LOCK_FREE != 2
#error "The signal-to-worker flag requires a lock-free atomic int"
#endif
static _Atomic int stopping;
static volatile sig_atomic_t exit_leader;
static int listeners[2] = {-1, -1};
static int term_fd;
static int port;
static int drain_seconds;
static const char *mode;
static const char *control;

static void fail(const char *what) {
    perror(what);
    exit(2);
}

static void pause_ms(long milliseconds) {
    struct timespec duration = {milliseconds / 1000, (milliseconds % 1000) * 1000000};
    while (nanosleep(&duration, &duration) < 0 && errno == EINTR) {}
}

static void record(const char *name) {
    char path[4096];
    if (snprintf(path, sizeof(path), "%s/%s", control, name) >= (int)sizeof(path)) exit(2);
    int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if (fd < 0 || write(fd, "yes\n", 4) != 4) fail("record");
    close(fd);
}

static int listen_at(const char *host, int requested_port) {
    int fd = socket(AF_INET, SOCK_STREAM, 0);
    int one = 1;
    if (fd < 0 || setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one)) < 0) fail("socket");
    struct sockaddr_in address = {.sin_family = AF_INET, .sin_port = htons(requested_port)};
    if (inet_pton(AF_INET, host, &address.sin_addr) != 1 ||
        bind(fd, (struct sockaddr *)&address, sizeof(address)) < 0 || listen(fd, 8) < 0) fail("listen");
    return fd;
}

static void on_term(int signal_number) {
    (void)signal_number;
    if (write(term_fd, "TERM\n", 5) != 5) _exit(2);
    atomic_store_explicit(&stopping, 1, memory_order_relaxed);
    if (!exit_leader) return;
    /* SYS_exit ends this thread, not the whole thread group. */
    syscall(SYS_exit, 0);
    _exit(2);
}

static void *drain(void *unused) {
    (void)unused;
    while (!atomic_load_explicit(&stopping, memory_order_relaxed)) pause_ms(10);
    if (strcmp(mode, "alive-empty") == 0) {
        for (int i = 0; i < 2; i++) if (listeners[i] >= 0) close(listeners[i]);
        record("closed");
        pause_ms(3000);
        _exit(0);
    }
    if (strcmp(mode, "replace") == 0) {
        pause_ms(1000);
        close(listeners[0]);
        listeners[0] = listen_at("127.0.0.2", port);
        record("replaced");
        pause_ms(120000);
        _exit(0);
    }
    for (int elapsed = 0; elapsed < drain_seconds; elapsed++) {
        pause_ms(1000);
        if (elapsed + 1 == drain_seconds / 2 && listeners[0] >= 0) {
            close(listeners[0]);
            listeners[0] = -1;
            record("first-closed");
        }
    }
    for (int i = 0; i < 2; i++) if (listeners[i] >= 0) close(listeners[i]);
    record("closed");
    /* A completed stop must not wait for this zombie leader to be reaped. */
    pause_ms(60000);
    _exit(0);
}

int main(int argc, char **argv) {
    if (argc == 2 && strcmp(argv[1], "free-port") == 0) {
        int fd = listen_at("127.0.0.2", 0);
        struct sockaddr_in address;
        socklen_t length = sizeof(address);
        if (getsockname(fd, (struct sockaddr *)&address, &length) < 0) fail("getsockname");
        printf("%u\n", (unsigned)ntohs(address.sin_port));
        close(fd);
        return 0;
    }
    if (argc != 3 || strcmp(argv[1], "-listen") != 0) return 2;
    const char *separator = strrchr(argv[2], ':');
    if (!separator || (port = atoi(separator + 1)) < 1 || port > 65535) return 2;
    control = getenv("FREENET_STOP_TEST_DIR");
    mode = getenv("FREENET_STOP_TEST_MODE");
    const char *delay = getenv("FREENET_STOP_TEST_DRAIN_SECONDS");
    if (!control || control[0] != '/' || !mode || !delay) return 2;
    exit_leader = strcmp(mode, "alive-empty") != 0;
    drain_seconds = atoi(delay);
    if (drain_seconds < 0 || drain_seconds > 180) return 2;
    char path[4096];
    if (snprintf(path, sizeof(path), "%s/terms", control) >= (int)sizeof(path)) return 2;
    term_fd = open(path, O_WRONLY | O_CREAT | O_APPEND, 0600);
    if (term_fd < 0) fail("terms");
    if (strcmp(mode, "empty") != 0) listeners[0] = listen_at("127.0.0.2", port);
    if (strcmp(mode, "empty") != 0 && strcmp(mode, "one") != 0) listeners[1] = listen_at("127.0.0.1", port);

    /* Only the leader receives TERM. Its worker inherits the blocked mask. */
    sigset_t blocked;
    sigemptyset(&blocked);
    sigaddset(&blocked, SIGTERM);
    if (pthread_sigmask(SIG_BLOCK, &blocked, NULL) != 0) return 2;
    struct sigaction action = {.sa_handler = on_term};
    sigemptyset(&action.sa_mask);
    if (sigaction(SIGTERM, &action, NULL) < 0) fail("sigaction");
    pthread_t worker;
    if (pthread_create(&worker, NULL, drain, NULL) != 0 ||
        pthread_sigmask(SIG_UNBLOCK, &blocked, NULL) != 0) return 2;
    record("ready");
    for (;;) pause();
}
