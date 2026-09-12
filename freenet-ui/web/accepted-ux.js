// accepted-ux loader: preserve the accepted baseline in accepted-ux-core.js,
// then mount the Automation page extension in deterministic order.
(() => {
  const load = (src) => {
    const script = document.createElement('script');
    script.src = src;
    script.async = false;
    document.head.appendChild(script);
  };
  load('/api/automation/assets/accepted-ux-core.js');
  load('/api/automation/assets/automation.js');
})();
