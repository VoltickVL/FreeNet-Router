from pathlib import Path
p=Path('freenet-ui/web/xray-core-manager.js');s=p.read_text()
a='  let topbarVersionAttempts = 0;\n'
b=a+'  let topbarRetryTimer = null;\n  let topbarReadController = null;\n  let topbarReadGeneration = 0;\n  const topbarMaxAttempts = 4;\n'
assert s.count(a)==1;s=s.replace(a,b)
a='    return chip;\n  }\n\n  async function refreshTopbarVersion()'
b='    if (serviceState) syncTopbarService(serviceState);\n    return chip;\n  }\n\n  async function refreshTopbarVersion()'
assert s.count(a)==1;s=s.replace(a,b)
a=s.index('  async function refreshTopbarVersion()');b=s.index('\n  function body()',a)
s=s[:a]+'''  function authenticatedTopbar() {
    if (typeof authAuthenticated === 'boolean') return authAuthenticated;
    return qs('#controlCenter')?.hidden === false;
  }

  function resetTopbarRead() {
    topbarReadGeneration += 1;
    clearTimeout(topbarRetryTimer);
    topbarRetryTimer = null;
    topbarReadController?.abort();
    topbarReadController = null;
    topbarVersionLoading = false;
    topbarVersionAttempts = 0;
  }

  async function refreshTopbarVersion() {
    if (!authenticatedTopbar() || topbarVersionLoading || topbarRetryTimer !== null || topbarVersionAttempts >= topbarMaxAttempts) return;
    const chip = mountTopbarChip();
    if (!chip) return; // The mount observer retries when the shell arrives.
    const generation = topbarReadGeneration;
    const controller = new AbortController();
    topbarReadController = controller;
    topbarVersionLoading = true;
    topbarVersionAttempts += 1;
    const timeout = setTimeout(() => controller.abort(), 5000);
    try {
      const response = await fetch('/api/xray/service', {cache:'no-store', signal:controller.signal});
      if (generation !== topbarReadGeneration || !authenticatedTopbar()) return;
      if (response.status === 401) {
        serviceState = null;
        syncTopbarVersion('вход требуется');
        return; // Authentication, not a timer, restarts the request budget.
      }
      const result = await response.json();
      if (generation !== topbarReadGeneration || !authenticatedTopbar()) return;
      if (!response.ok || !result?.success || (result.online !== false && !compactVersion(result.version || result.current_version))) {
        throw new Error('Xray status is not available');
      }
      chip.title = 'Управление Xray';
      // Background hydration never clears an unresolved mutation guard.
      syncServiceSurfaces(result);
    } catch (_) {
      if (generation !== topbarReadGeneration || !authenticatedTopbar()) return;
      if (!serviceState) syncTopbarVersion('недоступно');
      chip.title = 'Не удалось прочитать состояние Xray; это не означает остановку сервиса';
      if (topbarVersionAttempts < topbarMaxAttempts) {
        topbarRetryTimer = setTimeout(() => {
          topbarRetryTimer = null;
          refreshTopbarVersion();
        }, 700 * topbarVersionAttempts);
      }
    } finally {
      clearTimeout(timeout);
      if (generation === topbarReadGeneration && topbarReadController === controller) {
        topbarReadController = null;
        topbarVersionLoading = false;
      }
    }
  }

  function hydrateTopbar() {
    mountTopbarChip();
    if (!serviceState) refreshTopbarVersion();
  }

  function topbarAuthenticationChanged() {
    resetTopbarRead();
    serviceState = null;
    if (authenticatedTopbar()) {
      syncTopbarVersion('версия…');
      hydrateTopbar();
    } else {
      syncTopbarVersion('вход требуется');
    }
  }

  function resumeTopbarRead() {
    if (!authenticatedTopbar() || document.visibilityState === 'hidden') return;
    resetTopbarRead();
    refreshTopbarVersion();
  }
''' +s[b:]
a='''  const bootTopbar = () => {
    mountTopbarChip();
    refreshTopbarVersion();
    setTimeout(mountTopbarChip, 250);
    setTimeout(mountTopbarChip, 900);
  };'''
b='''  const bootTopbar = () => {
    hydrateTopbar();
    // A delayed shell or recreated topbar must hydrate independently of clicks.
    const observer = new MutationObserver(() => {
      if (!qs('#xrayTopbarChip') && qs('.topbar')) hydrateTopbar();
    });
    observer.observe(document.body, {childList:true, subtree:true});
  };
  document.addEventListener('freenet:auth-changed', topbarAuthenticationChanged);
  document.addEventListener('visibilitychange', resumeTopbarRead);
  window.addEventListener('online', resumeTopbarRead);'''
assert s.count(a)==1;s=s.replace(a,b)
s=s.replace("window.addEventListener('hashchange', mountTopbarChip);","window.addEventListener('hashchange', hydrateTopbar);")
p.write_text(s)
p=Path('freenet-ui/web/index.html');s=p.read_text();a='if(authAuthenticated)startControlCenter();else stopControlCenter()}';b="if(authAuthenticated)startControlCenter();else stopControlCenter();document.dispatchEvent(new CustomEvent('freenet:auth-changed',{detail:{authenticated:authAuthenticated}}))}"
assert s.count(a)==1;s=s.replace(a,b);p.write_text(s)
