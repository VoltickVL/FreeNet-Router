(() => {
  "use strict";
  const input = document.getElementById("admin-pair-code");
  const button = document.getElementById("admin-login");
  const feedback = document.getElementById("login-error");
  async function login() {
    const code = input?.value || "";
    if (code.length < 43 || !location.protocol.startsWith("https")) {
      feedback.textContent = "Доступен только защищённый вход HTTPS с кодом администратора.";
      return;
    }
    button.disabled = true;
    try {
      const result = await fetch("/api/auth/login", {
        method: "POST", cache: "no-store", credentials: "same-origin",
        headers: {"Content-Type": "application/json", "X-FreeNet-Action": "login"},
        redirect: "error",
        body: JSON.stringify({code})
      });
      if (!result.ok) throw new Error("auth-refused");
      window.location.replace("/");
    } catch (_) {
      feedback.textContent = "Код не принят или вход временно ограничен. Попробуйте позже.";
    } finally {
      input.value = "";
      button.disabled = false;
    }
  }
  button?.addEventListener("click",login);
  input?.addEventListener("keydown",e => {
    if (e.key==="Enter") {e.preventDefault();login();}
  });
})();
