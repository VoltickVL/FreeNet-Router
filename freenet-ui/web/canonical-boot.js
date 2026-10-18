// One bounded readiness gate for the complete canonical interface. No API
// status is treated as a DOM readiness signal; no router mutation is performed.
(() => {
  const root = document.documentElement;
  const canonicalRoutes = ['overview','settings','network','journal'];
  let timer = 0;
  let released = false;
  let started = 0;
  const canonicalNavReady = () => {
    const buttons = Array.from(document.querySelectorAll('.sidebar .nav>.nav-btn[data-page]'));
    return buttons.length === canonicalRoutes.length && buttons.every((button, index) =>
      (button.dataset.page === 'routing' ? 'network' : button.dataset.page) === canonicalRoutes[index] &&
      !!button.querySelector('.nav-icon[data-freenet-shell="1"] svg'));
  };
  const missingParts = () => {
    const parts = {
      styles: !!document.getElementById('freenetUnifiedDesignStyles'),
      brand: !!document.querySelector('.sidebar>.brand .fn-brand-lockup-svg'),
      navigation: canonicalNavReady(),
      overview: !!document.querySelector('.topbar.overview-approved #overviewApprovedTop') &&
        !!document.getElementById('bestCurrentHealth') &&
        document.querySelectorAll('#bestCurrentMetrics .best-v4-pill').length === 4,
      vpn: !!document.getElementById('fnVpnPickerV2Toggle'),
      versions: !!document.getElementById('xrayTopbarChip') && !!document.getElementById('topFreenetUpdate'),
      settings: !!document.getElementById('fn3AutoCard'),
      routing: !!document.getElementById('routingV2Workspace'),
      journal: !!document.querySelector('[data-page-view="journal"]') &&
        typeof window.openFreeNetJournal === 'function'
    };
    return Object.keys(parts).filter(key => !parts[key]);
  };
  const fail = missing => {
    root.dataset.freenetCanonicalBootStalled = '1';
    root.dataset.freenetCanonicalMissing = missing.join(',');
    delete root.dataset.freenetCanonicalReady;
    root.classList.add('freenet-canonical-boot-error');
    const app = document.getElementById('controlCenter');
    if (app) app.inert = true;
    if (document.getElementById('freenetBootFailure')) return;
    const panel = document.createElement('section');
    panel.id = 'freenetBootFailure';
    panel.className = 'fn-boot-error';
    panel.setAttribute('role','alert');
    panel.setAttribute('aria-labelledby','freenetBootErrorTitle');
    panel.innerHTML = '<h1 id="freenetBootErrorTitle">Интерфейс FreeNet загружен не полностью</h1>' +
      '<p>Настройки роутера не менялись. Обновите страницу. При повторной ошибке доступно независимое восстановление.</p>' +
      '<div class="fn-boot-error-actions"><button id="freenetBootRetry" type="button">Обновить страницу</button><a href="/recovery">Открыть Recovery</a></div>';
    panel.querySelector('button').addEventListener('click', () => location.reload(), {once:true});
    document.body.appendChild(panel);
    panel.querySelector('button').focus();
  };
  const release = () => {
    if (released) return;
    const missing = missingParts();
    if (!missing.length) {
      released = true;
      clearTimeout(timer);
      requestAnimationFrame(() => requestAnimationFrame(() => {
        root.classList.remove('freenet-canonical-boot');
        root.dataset.freenetCanonicalReady = '1';
      }));
      return;
    }
    if (Date.now() - started >= 12000) { fail(missing); return; }
    timer = setTimeout(release, 100);
  };
  const start = () => { started = Date.now(); release(); };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start, {once:true});
  else start();
})();
