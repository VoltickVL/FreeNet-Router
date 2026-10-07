;(() => {
  'use strict';
  if (window.__freenetSettingsV3Loaded) return;
  window.__freenetSettingsV3Loaded = true;

  const q = (s, r = document) => r.querySelector(s);
  const qa = (s, r = document) => Array.from(r.querySelectorAll(s));
  const state = {
    data: null, status: null, baseline: '', dirty: false, saving: false, checking: false, controlsBusy: false,
    countries: [], countryCatalog: [], countryCatalogFresh: false, countryCatalogLoading: false, countryCatalogWarning: '',
    journalFilter: 'all', journalResultFilter: 'all', journalQuery: '', journalEvents: [], journalLive: true, journalFiltersOpen: false,
    journalRefreshing: false, journalGeneratedAt: '', journalError: '', journalTimer: null,
    journalPage: 1, journalPages: 1, journalPageSize: 100, journalTotal: 0, journalFilteredTotal: 0,
    journalStats: {total:0,success:0,neutral:0,errors:0}, journalRetainedFrom: '', journalRetainedTo: '',
    journalDatePreset: '24h', journalCustomFrom: '', journalCustomTo: '', journalSearchTimer: null
  };

  const svg = (name) => {
    const paths = {
      settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6V21H10v-.1a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H3v-4h.1a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3A1.7 1.7 0 0 0 10 3h4a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.1v4H21a1.7 1.7 0 0 0-1.6 1Z"/>',
      vpn: '<circle cx="12" cy="12" r="9"/><path d="M7 12h10M9 8l-2 4 2 4M15 8l2 4-2 4"/>',
      info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>',
      check: '<path d="m5 12 4 4L19 6"/>',
      play: '<path d="m8 5 11 7-11 7Z"/>',
      globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.5 2.6 3.7 5.6 3.7 9S14.5 18.4 12 21M12 3C9.5 5.6 8.3 8.6 8.3 12S9.5 18.4 12 21"/>',
      box: '<path d="m4 7 8-4 8 4-8 4Z"/><path d="M4 7v10l8 4 8-4V7M12 11v10"/>',
      backup: '<ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v6c0 1.7 3.6 3 8 3s8-1.3 8-3V5M4 11v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6"/>',
      refresh: '<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 5v6h-6"/>',
      clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
      signal: '<path d="M4 18v-2M8 18v-5M12 18V9M16 18V6M20 18V3"/>',
      speed: '<path d="M4 17a8 8 0 1 1 16 0"/><path d="m12 13 4-4M8 17h8"/>',
      shield: '<path d="M12 3 5 6v5c0 4.6 2.8 8 7 10 4.2-2 7-5.4 7-10V6Z"/><path d="m9 12 2 2 4-4"/>',
      copy: '<rect x="9" y="9" width="10" height="10" rx="2"/><path d="M15 9V7a2 2 0 0 0-2-2H7a2 2 0 0 0-2 2v6a2 2 0 0 0 2 2h2"/>',
      chevron: '<path d="m9 18 6-6-6-6"/>',
      save: '<path d="M5 4h12l2 2v14H5Z"/><path d="M8 4v6h8V4M8 20v-6h8v6"/>',
      download: '<path d="M12 3v12M7 10l5 5 5-5M5 20h14"/>',
      upload: '<path d="M12 21V9M7 14l5-5 5 5M5 4h14"/>',
      subscription: '<rect x="4" y="6" width="16" height="13" rx="3"/><path d="M8 3v6M16 3v6M4 11h16"/>'
    };
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths[name] || paths.info}</svg>`;
  };

  function injectStyles() {
    if (q('#freenetSettingsV3Styles')) return;
    const style = document.createElement('style');
    style.id = 'freenetSettingsV3Styles';
    style.textContent = `
      body:has([data-page-view="settings"].active) .content{width:min(1460px,calc(100% - 48px))!important;padding-top:14px!important}
      body:has([data-page-view="journal"].active) .content{width:min(1460px,calc(100% - 48px))!important;padding-top:20px!important}
      body:has([data-page-view="settings"].active) #pageTitle{display:none!important}
      .fn3-page{display:block!important}.fn3-head{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin:0 0 13px}.fn3-head h1{font-size:30px;line-height:1.05;margin:0;letter-spacing:-.04em}.fn3-head p{margin:6px 0 0;color:#8fa7c3;font-size:13px}.fn3-save{min-width:170px;min-height:40px!important;gap:7px;justify-content:center!important;font-size:12px!important}.fn3-save svg,.fn3-extra-save svg{width:16px!important;height:16px!important;flex:0 0 16px!important}.fn3-save[disabled]{opacity:.52;filter:saturate(.7)}
      .fn3-grid{display:grid;grid-template-columns:minmax(0,1fr);gap:14px;align-items:start}.fn3-left{display:grid;grid-template-columns:minmax(0,1fr);gap:14px;width:100%;min-width:0;grid-column:1/-1}.fn3-extra{width:100%;min-width:0}.fn3-card{border:1px solid #244a6d;border-radius:14px;background:linear-gradient(180deg,rgba(12,31,52,.98),rgba(7,22,38,.99));padding:15px;box-shadow:0 12px 32px rgba(0,0,0,.12);min-width:0}.fn3-card-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.fn3-title{display:flex;align-items:flex-start;gap:11px}.fn3-icon{display:grid;place-items:center;width:36px;height:36px;flex:0 0 36px;border-radius:10px;background:linear-gradient(180deg,#1763d0,#114692);color:#b8d6ff}.fn3-icon svg{width:22px;height:22px}.fn3-card h2{font-size:19px;margin:0;letter-spacing:-.02em}.fn3-card h3{font-size:15px;margin:0}.fn3-sub{margin-top:3px;color:#8fa6c0;font-size:12px;line-height:1.4}
      .fn3-master{display:flex;align-items:center;gap:9px;font-size:12px;font-weight:750;color:#eef5ff}.fn3-switch{position:relative;width:45px;height:24px;display:inline-block;flex:0 0 45px}.fn3-switch input{position:absolute;opacity:0;pointer-events:none}.fn3-switch span{position:absolute;inset:0;border-radius:999px;background:#3b4c61;border:1px solid #586a80;transition:.15s}.fn3-switch span:after{content:'';position:absolute;top:2px;left:2px;width:18px;height:18px;border-radius:50%;background:#eef5fc;transition:.15s;box-shadow:0 2px 5px #0006}.fn3-switch input:checked+span{background:#18c78c;border-color:#34e3a8}.fn3-switch input:checked+span:after{transform:translateX(21px);background:white}
      .fn3-info{display:flex;gap:10px;align-items:flex-start;margin-top:12px;padding:11px 12px;border:1px solid #2c6093;border-radius:10px;background:rgba(23,75,132,.23);font-size:12px;line-height:1.48;color:#bed0e4}.fn3-info svg{width:19px;height:19px;flex:0 0 19px;color:#69a9ff}.fn3-section-label{margin:13px 0 7px;color:#f1f6ff;font-size:13px;font-weight:800}.fn3-mode-list{display:grid;grid-template-columns:1fr 1fr;gap:9px}.fn3-mode{display:grid;grid-template-columns:auto 1fr;gap:10px;align-items:flex-start;padding:12px;border:1px solid #295174;border-radius:11px;background:#081b2f;cursor:pointer;min-height:82px;transition:.15s}.fn3-mode:hover{border-color:#3971a4;background:#0a2139}.fn3-mode.selected{border-color:#2f8cf8;background:linear-gradient(180deg,#0d345c,#0a2a4b);box-shadow:inset 0 0 0 1px #2f8cf8,0 8px 24px rgba(15,78,143,.16)}.fn3-mode input{width:18px;height:18px;margin-top:2px;accent-color:#31e1a3}.fn3-mode strong{display:block;font-size:13px;color:#eef6ff}.fn3-mode small{display:block;color:#9ab0c9;font-size:11.5px;line-height:1.42;margin-top:4px}.fn3-mode-badge{display:inline-block;margin-left:6px;padding:2px 6px;border-radius:999px;background:#123c5f;color:#a9d5ff;font-size:9.5px;font-style:normal}.fn3-endpoint-schedule{display:flex;align-items:center;justify-content:space-between;gap:14px;margin-top:9px;padding:10px 12px;border:1px solid #2b5f91;border-radius:9px;background:rgba(10,39,68,.72)}.fn3-endpoint-schedule[hidden],.fn3-replacement[hidden]{display:none!important}.fn3-endpoint-copy strong{display:block;color:#dceaff;font-size:11.5px}.fn3-endpoint-copy small{display:block;margin-top:2px;color:#8fa8c2;font-size:10.5px}.fn3-endpoint-schedule select{min-width:190px;height:34px;border:1px solid #315777;border-radius:8px;background:#071a2c;color:#edf5ff;padding:0 9px;font-size:11px}.fn3-scope-list{border:1px solid #295174;border-radius:11px;overflow:hidden;background:#081b2f}.fn3-scope{display:grid;grid-template-columns:auto 1fr auto;gap:11px;align-items:center;padding:10px 12px;border-top:1px solid #254765;cursor:pointer;min-height:58px}.fn3-scope:first-child{border-top:0}.fn3-scope.selected{background:linear-gradient(180deg,#0d345c,#0a2a4b);box-shadow:inset 0 0 0 1px #2f8cf8}.fn3-scope input{width:18px;height:18px;accent-color:#31e1a3}.fn3-scope strong{display:block;font-size:13px}.fn3-scope small{display:block;color:#9ab0c9;font-size:11.5px;margin-top:3px}.fn3-recommended{display:inline-block;margin-left:6px;padding:2px 6px;border-radius:999px;background:#0b6e50;color:#67f1bc;font-size:10px}.fn3-chevron{width:18px;color:#7fb7f8}.fn3-safety{display:flex;gap:9px;align-items:flex-start;margin-top:10px;padding:10px 11px;border:1px solid #2b5f91;border-radius:9px;color:#b7cbe1;font-size:11.5px;line-height:1.42}.fn3-safety svg{width:18px;flex:0 0 18px;color:#6daeff}.fn3-auto-actions{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,.78fr);gap:10px;margin-top:11px}.fn3-auto-actions .btn{min-height:44px!important;justify-content:center!important}.fn3-control{display:flex;align-items:center;gap:9px;padding:9px 11px;border:1px solid #275174;border-radius:10px;background:#091b2e;color:#c6d6e8}.fn3-control-dot{width:20px;height:20px;border-radius:50%;border:4px solid #0b4c3a;background:#26d69b;box-shadow:0 0 0 2px #179d76}.fn3-control strong{display:block;color:#4ee3aa;font-size:12px}.fn3-control small{display:block;color:#879eb8;font-size:10.5px;margin-top:2px}
      .fn3-vpn-head{display:flex;align-items:center;justify-content:space-between}.fn3-status-pill{padding:5px 10px;border-radius:999px;background:#075b43;color:#55efb2;font-size:10px;font-weight:800}.fn3-profile{margin-top:10px;border:1px solid #294f70;border-radius:10px;background:#07192a;padding:12px}.fn3-profile-top{display:flex;align-items:center;gap:10px}.fn3-flag{font-size:22px;line-height:1}.fn3-profile-name{font-size:18px;font-weight:850}.fn3-profile-facts{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin-top:10px}.fn3-fact{padding-left:10px;border-left:1px solid #315170}.fn3-fact span{display:block;color:#8ba2bd;font-size:10px}.fn3-fact strong{display:block;margin-top:4px;font-size:12px}.fn3-endpoint{display:flex;align-items:center;gap:7px}.fn3-copy{border:0;background:transparent;color:#62a6f6;padding:2px;cursor:pointer}.fn3-copy svg{width:17px;height:17px}.fn3-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:7px;margin-top:9px}.fn3-metric{border:1px solid #294f70;border-radius:9px;background:#07192a;padding:8px;min-height:55px}.fn3-metric span{display:flex;align-items:center;gap:5px;color:#8da5c0;font-size:10px}.fn3-metric svg{width:14px;color:#66a8ff}.fn3-metric strong{display:block;margin-top:4px;font-size:12px}.fn3-health{display:flex;align-items:center;gap:9px;margin-top:9px;padding:9px 11px;border:1px solid #16815e;border-radius:9px;background:#073c30;color:#5ee7af;font-size:11.5px;font-weight:700}.fn3-health svg{width:18px}.fn3-health small{display:block;color:#b1d9ca;font-weight:500;margin-top:2px;font-size:10.5px}
      .fn3-journal-linkrow{display:flex;justify-content:flex-end;margin-top:10px;padding-top:10px;border-top:1px solid #244663}.fn3-link{border:0;background:transparent;color:#9dc7f2;font-size:12px;cursor:pointer}.fn3-link:hover{color:#c9e2ff;text-decoration:underline}.fn3-table{width:100%;border-collapse:collapse;font-size:12.5px;line-height:1.42}.fn3-table th{text-align:left;padding:9px 10px;background:#123655;color:#b9cce0;font-size:11.5px;font-weight:800}.fn3-table td{padding:9px 10px;border-top:1px solid #254765;color:#d8e4ef;vertical-align:top}.fn3-table td:first-child{white-space:nowrap;color:#a9bfd6}.fn3-table td:nth-child(2){font-weight:720;color:#c8d8e8}.fn3-kind{display:inline-flex;align-items:center;min-height:24px;padding:3px 8px;border:1px solid #355878;border-radius:999px;background:#0a2035;color:#bdd0e4;font-size:10.5px;font-weight:800;white-space:nowrap}.fn3-kind.auto{border-color:#2e679a;color:#a9cfff;background:#0b2947}.fn3-kind.system{border-color:#41627f;color:#c4d1df;background:#112339}.fn3-result{display:inline-flex;align-items:center;gap:6px;min-height:24px;padding:3px 8px;border:1px solid rgba(82,228,168,.26);border-radius:999px;background:rgba(35,124,90,.16);color:#52e4a8;font-weight:760;white-space:nowrap}.fn3-result.neutral{border-color:#3c5875;background:#102239;color:#c4d0dc}.fn3-result.bad{border-color:rgba(255,103,115,.4);background:rgba(102,34,45,.24);color:#ff9da7}.fn3-dot{width:7px;height:7px;border-radius:50%;background:currentColor}
      .fn3-extra{grid-column:1/-1}.fn3-extra-title{margin-bottom:10px}.fn3-extra-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px}.fn3-extra-card{border:1px solid #2c5274;border-radius:11px;background:#081b2f;padding:11px;min-width:0}.fn3-extra-head{display:grid;grid-template-columns:auto 1fr auto;gap:9px;align-items:start}.fn3-extra-icon{display:grid;place-items:center;width:34px;height:34px;border-radius:9px;background:#1555a5;color:#b4d4ff}.fn3-extra-icon svg{width:20px;height:20px}.fn3-extra-card h3{font-size:12px}.fn3-extra-card p{margin:3px 0 0;color:#8fa5bf;font-size:10px;line-height:1.4}.fn3-extra-row{display:grid;grid-template-columns:auto minmax(0,1fr);gap:8px;align-items:center;margin-top:10px}.fn3-extra-row label{color:#9eb2c9;font-size:10.5px}.fn3-extra-row select{height:31px;border:1px solid #315777;border-radius:7px;background:#091a2b;color:#e7eff9;padding:0 8px;font-size:11px}.fn3-extra-meta{display:grid;grid-template-columns:auto 1fr;gap:7px;margin-top:7px;font-size:10px}.fn3-extra-meta span{color:#879db7}.fn3-extra-meta b{color:#dce7f2}.fn3-extra-meta b.ok{color:#4de0a5}.fn3-extra-action{width:100%;margin-top:9px;min-height:34px!important;justify-content:center!important;font-size:11px!important}.fn3-extra-save-row{display:flex;justify-content:flex-end;margin-top:10px;padding-top:10px;border-top:1px solid #244663}.fn3-extra-save{min-width:170px;min-height:36px!important;gap:7px;justify-content:center!important;font-size:11px!important}.fn3-extra-save[disabled]{opacity:.52;filter:saturate(.7)}.fn3-backup-storage{display:grid;grid-template-columns:auto minmax(0,1fr);gap:6px 8px;margin-top:9px;padding:9px 10px;border:1px solid #2b506f;border-radius:9px;background:#07192a;font-size:10px}.fn3-backup-storage span{color:#879db7}.fn3-backup-storage code{color:#d9e7f6;font:600 10px/1.35 ui-monospace,SFMono-Regular,Menlo,monospace;overflow-wrap:anywhere;word-break:break-word}.fn3-backup-contents{margin-top:8px;color:#8fa7c1;font-size:10px;line-height:1.42}.fn3-backup-result{display:grid;gap:4px;margin-top:9px;padding:9px 10px;border:1px solid #16815e;border-radius:9px;background:#073c30;color:#cdeee0;font-size:10.5px;line-height:1.4}.fn3-backup-result[hidden]{display:none!important}.fn3-backup-result.bad{border-color:#9b3b51;background:#351725;color:#ffd4dc}.fn3-backup-result strong{font-size:11.5px;color:#5ee7af}.fn3-backup-result.bad strong{color:#ffb1bf}.fn3-backup-result code{font:600 10px/1.4 ui-monospace,SFMono-Regular,Menlo,monospace;overflow-wrap:anywhere;word-break:break-word;color:#eef7ff}.fn3-backup-actions{display:grid;grid-template-columns:1fr 1fr;gap:7px;margin-top:9px}.fn3-backup-actions .btn{min-height:34px!important;justify-content:center!important;font-size:10.5px!important}.fn3-danger{border-color:#9b3b51!important;color:#ffc0cc!important;background:#351725!important}
      .fn3-country-pop{position:fixed;z-index:1900;width:min(520px,calc(100vw - 28px));max-height:min(620px,calc(100vh - 36px));overflow:auto;padding:14px;border:1px solid #35638e;border-radius:14px;background:#091c30;box-shadow:0 24px 74px #0009}.fn3-country-pop[hidden]{display:none!important}.fn3-country-list{display:grid;grid-template-columns:1fr 1fr;gap:7px;margin-top:10px}.fn3-country-item{display:flex;align-items:center;gap:9px;padding:9px 10px;border:1px solid #294f70;border-radius:8px;font-size:11.5px;min-height:42px}.fn3-country-item input{accent-color:#2fdfa1}.fn3-country-flag{width:24px!important;height:16px!important;flex:0 0 24px!important;border-radius:3px!important}.fn3-country-copy{display:flex;flex-direction:column;gap:2px;min-width:0}.fn3-country-copy small{font-size:9.5px;color:#8098b2}.fn3-country-item.unavailable{border-style:dashed;opacity:.78}.fn3-country-state{margin-top:10px;padding:10px 11px;border:1px solid #2a567d;border-radius:8px;background:#07192a;color:#9eb5cf;font-size:11px}.fn3-pop-actions{display:flex;justify-content:flex-end;gap:8px;margin-top:11px}.fn3-compat{display:none!important}
      .fn3-journal-page{padding-bottom:34px}
      .fn3-journal-hero{position:relative;overflow:hidden;border:1px solid #294f73;border-radius:20px;background:radial-gradient(circle at 85% 10%,rgba(45,126,255,.16),transparent 34%),linear-gradient(145deg,#0c2138 0%,#091827 58%,#081522 100%);padding:24px 26px;box-shadow:0 20px 52px rgba(0,0,0,.18)}
      .fn3-journal-hero:after{content:'';position:absolute;right:-54px;bottom:-92px;width:220px;height:220px;border-radius:50%;border:1px solid rgba(92,154,255,.12)}
      .fn3-journal-kicker{color:#6da8ff;font-size:10px;font-weight:900;letter-spacing:.16em;text-transform:uppercase}
      .fn3-journal-hero h1{font-size:34px;line-height:1.05;margin:7px 0 0;letter-spacing:-.045em;color:#f7fbff}
      .fn3-journal-hero p{max-width:820px;color:#9db2ca;margin:8px 0 0;font-size:13.5px;line-height:1.55}
      .fn3-journal-policy{display:flex;flex-wrap:wrap;gap:8px;margin-top:16px}
      .fn3-journal-policy span{display:inline-flex;align-items:center;gap:7px;min-height:30px;padding:6px 10px;border:1px solid #315675;border-radius:999px;background:rgba(5,19,33,.72);color:#b8cbe0;font-size:10.5px;font-weight:750}
      .fn3-journal-policy span:before{content:'';width:6px;height:6px;border-radius:50%;background:#5f9eff}
      .fn3-journal-policy .healthy:before{background:#47dca2}
      .fn3-journal-control-card{margin-top:14px;border:1px solid #284b6b;border-radius:16px;background:linear-gradient(180deg,rgba(10,29,48,.98),rgba(7,22,37,.99));padding:14px 15px;box-shadow:0 14px 36px rgba(0,0,0,.12)}
      .fn3-journal-toolbar{display:grid;grid-template-columns:minmax(300px,1fr) auto;gap:10px;align-items:center}
      .fn3-journal-search{height:42px;border:1px solid #315777;border-radius:11px;background:#071827;color:#eef5ff;padding:0 13px;font:600 12px/1.2 inherit;outline:none}
      .fn3-journal-search:focus{border-color:#438ff0;box-shadow:0 0 0 3px rgba(47,140,248,.14)}
      .fn3-journal-actions{display:flex;align-items:center;gap:8px}
      .fn3-journal-live,.fn3-journal-refresh,.fn3-journal-export,.fn3-journal-filter-toggle{min-height:38px;padding:7px 12px;border:1px solid #315777;border-radius:10px;background:#0a1d31;color:#c5d7e9;font:750 11px/1.2 inherit;cursor:pointer}
      .fn3-journal-export{border-color:#2b70ba;background:#0d3155;color:#d8ecff}
      .fn3-journal-live.active{border-color:#198563;color:#6aebba;background:#07382e}
      .fn3-journal-filter-toggle.active{border-color:#347fd0;background:#11365b;color:#f0f7ff}.fn3-journal-filter-count{display:inline-grid;place-items:center;min-width:18px;height:18px;margin-left:6px;padding:0 5px;border-radius:999px;background:#2f8cf8;color:#fff;font-size:9px;font-weight:900}.fn3-journal-filter-groups[hidden]{display:none!important}
      .fn3-journal-live-dot{display:inline-block;width:7px;height:7px;margin-right:6px;border-radius:50%;background:currentColor;vertical-align:1px}
      .fn3-journal-meta{margin-top:8px;color:#7e98b4;font-size:10.5px;line-height:1.45}.fn3-journal-meta.bad{color:#ff9da7}
      .fn3-journal-filter-groups{display:grid;gap:8px;margin-top:13px;padding-top:12px;border-top:1px solid #203f5c}
      .fn3-journal-filter-row{display:flex;flex-wrap:wrap;gap:8px;align-items:center}.fn3-journal-filter-title{min-width:72px;color:#7892ad;font-size:10px;font-weight:850;text-transform:uppercase;letter-spacing:.06em}
      .fn3-journal-filters,.fn3-journal-range{display:flex;flex-wrap:wrap;gap:7px;align-items:center}
      .fn3-journal-filter{min-height:32px;padding:6px 11px;border:1px solid #2d4f6e;border-radius:999px;background:#091a2c;color:#aebfd2;font:720 10.5px/1.2 inherit;cursor:pointer}
      .fn3-journal-filter:hover{border-color:#44739f;color:#e1edf8}.fn3-journal-filter.active{border-color:#347fd0;background:#11365b;color:#f0f7ff;box-shadow:inset 0 0 0 1px rgba(61,139,226,.22)}
      .fn3-journal-range input,.fn3-journal-page-size{height:32px;border:1px solid #315777;border-radius:8px;background:#071827;color:#eaf3ff;padding:0 9px;font:700 10.5px/1.2 inherit}
      .fn3-journal-custom{display:flex;gap:7px;align-items:center}.fn3-journal-custom[hidden]{display:none!important}
      .fn3-journal-summary{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px;margin-top:14px}
      .fn3-journal-stat{position:relative;overflow:hidden;padding:13px 14px;border:1px solid #284c6c;border-radius:14px;background:linear-gradient(180deg,#0b2035,#091a2b);min-height:74px}
      .fn3-journal-stat:after{content:'';position:absolute;right:-18px;bottom:-26px;width:72px;height:72px;border-radius:50%;background:rgba(103,156,216,.05)}
      .fn3-journal-stat span{display:block;color:#879fba;font-size:10px;font-weight:750}.fn3-journal-stat strong{display:block;margin-top:7px;color:#f3f8fd;font-size:22px;letter-spacing:-.025em}.fn3-journal-stat.ok strong{color:#59e0aa}.fn3-journal-stat.bad strong{color:#ff9aa5}
      .fn3-journal-stream-card{margin-top:14px;border:1px solid #284b6b;border-radius:16px;background:linear-gradient(180deg,#0a1d30,#071725);padding:14px;box-shadow:0 16px 40px rgba(0,0,0,.12)}
      .fn3-journal-stream-head{display:flex;justify-content:space-between;gap:14px;align-items:flex-end;padding:2px 2px 12px}.fn3-journal-stream-head h2{margin:0;color:#f3f7fb;font-size:16px}.fn3-journal-stream-head p{margin:4px 0 0;color:#7892ad;font-size:10.5px}
      .fn3-journal-event-list{display:grid;gap:8px}
      .fn3-journal-event{display:grid;grid-template-columns:10px minmax(0,1fr);gap:11px;padding:12px 13px;border:1px solid #244762;border-radius:12px;background:#081a2a;transition:border-color .15s,background .15s,transform .15s}
      .fn3-journal-event:hover{border-color:#35678f;background:#0a2034;transform:translateY(-1px)}
      .fn3-journal-event-rail{display:flex;flex-direction:column;align-items:center;padding-top:5px}.fn3-journal-event-dot{width:8px;height:8px;border-radius:50%;background:#7892ad;box-shadow:0 0 0 4px rgba(120,146,173,.08)}.fn3-journal-event.ok .fn3-journal-event-dot{background:#45dba1}.fn3-journal-event.bad .fn3-journal-event-dot{background:#ff7583}
      .fn3-journal-event-main{min-width:0}.fn3-journal-event-top{display:flex;justify-content:space-between;gap:12px;align-items:flex-start}.fn3-journal-event-tags{display:flex;flex-wrap:wrap;gap:6px;align-items:center}.fn3-journal-event-time{flex:0 0 auto;color:#7892ad;font-size:10.5px;font-weight:700;white-space:nowrap}
      .fn3-journal-event-message{margin-top:8px;color:#d9e5ef;font-size:12.5px;line-height:1.5;overflow-wrap:anywhere}
      .fn3-kind{display:inline-flex;align-items:center;min-height:24px;padding:3px 8px;border:1px solid #355878;border-radius:999px;background:#0a2035;color:#bdd0e4;font-size:10px;font-weight:800;white-space:nowrap}.fn3-kind.auto{border-color:#2e679a;color:#a9cfff;background:#0b2947}.fn3-kind.system{border-color:#41627f;color:#c4d1df;background:#112339}.fn3-kind.vpn{border-color:#356c9e;color:#b8d7f7;background:#0b2946}
      .fn3-result{display:inline-flex;align-items:center;gap:6px;min-height:24px;padding:3px 8px;border:1px solid rgba(82,228,168,.26);border-radius:999px;background:rgba(35,124,90,.16);color:#52e4a8;font-size:10px;font-weight:780;white-space:nowrap}.fn3-result.neutral{border-color:#3c5875;background:#102239;color:#c4d0dc}.fn3-result.bad{border-color:rgba(255,103,115,.4);background:rgba(102,34,45,.24);color:#ff9da7}.fn3-dot{width:6px;height:6px;border-radius:50%;background:currentColor}
      .fn3-journal-empty{padding:34px 18px;text-align:center;border:1px dashed #31526f;border-radius:12px;color:#8198b2;background:#071726;font-size:12px}
      .fn3-journal-pager{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-top:12px;padding-top:12px;border-top:1px solid #203f5c}.fn3-journal-pager-controls{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.fn3-journal-page-btn{min-height:32px;padding:6px 10px;border:1px solid #315777;border-radius:8px;background:#091c30;color:#c9d9e8;font:700 10.5px/1.2 inherit;cursor:pointer}.fn3-journal-page-btn[disabled]{opacity:.4;cursor:default}.fn3-journal-page-label{color:#9eb3ca;font-size:10.5px}.fn3-journal-retention{color:#718aa5;font-size:10px;line-height:1.4}
      @media(max-width:1120px){.fn3-extra{grid-column:auto}.fn3-extra-grid{grid-template-columns:1fr 1fr}.fn3-journal-summary{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:760px){.fn3-head{display:block}.fn3-save{width:100%;margin-top:10px}.fn3-extra-save-row{display:block}.fn3-extra-save{width:100%;min-width:0}.fn3-mode-list{grid-template-columns:1fr}.fn3-endpoint-schedule{align-items:flex-start;flex-direction:column}.fn3-endpoint-schedule select{width:100%;min-width:0}.fn3-auto-actions,.fn3-profile-facts{grid-template-columns:1fr}.fn3-metrics{grid-template-columns:1fr 1fr}.fn3-extra-grid{grid-template-columns:1fr}.fn3-country-list{grid-template-columns:1fr}.fn3-table{font-size:11.5px}.fn3-table th,.fn3-table td{padding:8px 7px}.fn3-journal-toolbar{grid-template-columns:1fr}.fn3-journal-actions{justify-content:stretch}.fn3-journal-live,.fn3-journal-refresh{flex:1}.fn3-journal-filter-title{width:100%;min-width:0}.fn3-journal-page .fn3-card{overflow-x:auto}}
    `;
    document.head.appendChild(style);
  }

  function ensureSettingsNav() {
    const nav = q('.sidebar .nav');
    if (!nav) return null;
    q('.nav-btn[data-page="automation"]', nav)?.remove();
    let button = q('.nav-btn[data-page="settings"]', nav);
    if (!button) {
      button = document.createElement('button');
      button.type = 'button';
      button.className = 'nav-btn';
      button.dataset.page = 'settings';
      button.innerHTML = `<span class="nav-icon">${svg('settings')}</span><span>Настройки</span>`;
      const routing = q('.nav-btn[data-page="routing"],.nav-btn[data-page="network"]', nav);
      if (routing) nav.insertBefore(button, routing);
      else nav.appendChild(button);
      button.addEventListener('click', () => {
        if (typeof window.setPage === 'function') window.setPage('settings');
      });
    }
    try { if (typeof pageLabels === 'object' && pageLabels) pageLabels.settings = 'Настройки'; } catch (_) {}
    return button;
  }

  function rewireNavigation() {
    const nav = q('.sidebar .nav');
    if (!nav) return;
    ensureSettingsNav();
    qa('.nav-btn', nav).forEach(btn => {
      const page = btn.dataset.page;
      if (page === 'system') btn.remove();
    });
    let journal = q('.nav-btn[data-page="journal"]', nav);
    if (!journal) {
      journal = document.createElement('button');
      journal.type = 'button';
      journal.className = 'nav-btn';
      journal.dataset.page = 'journal';
      journal.innerHTML = `<span class="nav-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="4" y="4" width="16" height="16" rx="2"/><path d="M8 8h8M8 12h8M8 16h6"/></svg></span><span>Журнал</span>`;
      nav.appendChild(journal);
    }
    if (journal.dataset.freenetJournalBound !== '1') {
      journal.dataset.freenetJournalBound = '1';
      journal.addEventListener('click', () => {
        if (typeof window.setPage === 'function') window.setPage('journal');
        mountJournalPage();
      });
    }
    if (!q('[data-page-view="journal"]')) {
      const page = document.createElement('section');
      page.className = 'page fn3-journal-page';
      page.dataset.pageView = 'journal';
      const content = q('.content');
      if (content) content.appendChild(page);
    }
  }

  const formatDate = (value, compact = false) => {
    if (!value) return '—';
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return '—';
    const p = new Intl.DateTimeFormat('ru-RU', {day:'2-digit',month:'2-digit',year:'numeric',hour:'2-digit',minute:'2-digit'}).format(d).replace(',', '');
    return compact ? p : p;
  };

  function intervalLabel(value) {
    return ({'30m':'30 минут','1h':'1 час','3h':'3 часа','6h':'6 часов','12h':'12 часов','24h':'24 часа'})[value] || '6 часов';
  }

  function healthIntervalLabel(value) {
    return ({'30s':'30 секунд','1m':'1 минуту','5m':'5 минут'})[value] || '1 минуту';
  }

  function healthIntervalOptions(selected) {
    return ['30s','1m','5m'].map(v => `<option value="${v}"${v===selected?' selected':''}>каждые ${healthIntervalLabel(v)}</option>`).join('');
  }

  function nextLabel(value) {
    if (!value) return 'не запланировано';
    const ms = new Date(value).getTime() - Date.now();
    if (!Number.isFinite(ms)) return formatDate(value);
    if (ms <= 0) return 'в ближайшее время';
    if (ms < 60000) return `через ${Math.max(1, Math.ceil(ms / 1000))} сек`;
    const min = Math.max(1, Math.ceil(ms / 60000));
    if (min < 60) return `через ${min} мин`;
    const h = Math.floor(min / 60), m = min % 60;
    return `через ${h} ч${m ? ` ${m} мин` : ''}`;
  }

  function cleanProfileLabel(value) {
    const text = String(value || '').trim();
    const withoutISO = text.replace(/^[A-Za-z]{2}\s+/, '').trim();
    const chars = Array.from(withoutISO);
    if (chars.length >= 2 && chars[0].codePointAt(0) >= 0x1F1E6 && chars[0].codePointAt(0) <= 0x1F1FF && chars[1].codePointAt(0) >= 0x1F1E6 && chars[1].codePointAt(0) <= 0x1F1FF) return chars.slice(2).join('').trim();
    return withoutISO;
  }

  function setCountryFlag(node, code) {
    if (!node) return;
    const cc = String(code || '').trim().toLowerCase();
    Array.from(node.classList).filter(name => name.startsWith('flag-')).forEach(name => node.classList.remove(name));
    if (/^[a-z]{2}$/.test(cc)) {
      node.classList.add('flag-icon', `flag-${cc}`);
      node.setAttribute('aria-label', cc.toUpperCase());
      node.textContent = '';
    } else {
      node.classList.remove('flag-icon');
      node.removeAttribute('aria-label');
      node.textContent = '🌐';
    }
  }

  function countryFlagMarkup(code) {
    const cc = String(code || '').trim().toLowerCase();
    return /^[a-z]{2}$/.test(cc) ? `<span class="flag-icon flag-${cc} fn3-country-flag" aria-hidden="true"></span>` : `<span class="fn3-country-flag">🌐</span>`;
  }

  function humanResult(result, message) {
    const full = String(result || '').trim().toLowerCase();
    const raw = full.includes(':') ? full.slice(full.lastIndexOf(':') + 1) : full;
    const msg = String(message || '');
    if (full === 'selection') return ['Решение', translateMessage(msg), 'neutral'];
    if (raw === 'start' || raw === 'started') return [/(обновлен|update)/i.test(msg) ? 'Обновление' : 'Запущено', translateMessage(msg), 'neutral'];
    if (raw === 'success' || raw === 'healthy' || raw === 'switched' || raw === 'updated' || raw === 'cleared') return ['Успешно', translateMessage(msg), 'ok'];
    if (raw === 'same' || raw === 'no_new' || raw === 'candidate' || raw === 'disabled' || raw === 'blocked' || raw === 'cooldown') return ['Без изменений', translateMessage(msg), 'neutral'];
    if (raw === 'uncertain' || raw === 'busy' || raw === 'degraded' || raw === 'recovery_allowed' || raw === 'reconciled_failed') return ['Пропущено', translateMessage(msg), 'neutral'];
    if (raw === 'failed' || raw === 'critical' || raw === 'rollback_failed') return ['Ошибка', translateMessage(msg), 'bad'];
    return ['Без изменений', translateMessage(msg), 'neutral'];
  }

  function translateMessage(message) {
    const m = String(message || '').trim();
    if (!m) return 'Операция завершена.';
    const lower = m.toLowerCase();
    if (lower.includes('fresh subscription has no endpoint for the current logical profile')) return 'Для текущего VPN в подписке нет нового адреса подключения.';
    if (lower.includes('current vpn') && lower.includes('does not require')) return 'Текущий VPN работает нормально, смена не требуется.';
    if (lower.includes('automation') && lower.includes('disabled')) return 'Автоматика выключена.';
    if (lower.includes('no endpoint')) return 'Для текущего VPN нет нового адреса подключения.';
    return m.replace(/logical profile/gi, 'текущего VPN').replace(/endpoint/gi, 'адрес подключения').replace(/Eligible/gi, 'проверенный');
  }

  function pageMarkup() {
    return `
      <div class="fn3-head"><div><h1>Настройки / Система</h1><p>Автоматизация, обновления и резервное копирование FreeNet.</p></div><button id="fn3Save" class="btn primary fn3-save" type="button" disabled>${svg('save')}<span>Сохранено</span></button></div>
      <div class="fn3-grid">
        <div class="fn3-left">
          <section class="fn3-card">
            <div class="fn3-card-head"><div class="fn3-title"><span class="fn3-icon">${svg('vpn')}</span><div><h2>AUTO VPN</h2><span id="fn3AutoLabel" class="fn3-enabled-badge">Выключено</span><div class="fn3-sub">FreeNet автоматически поддерживает рабочий VPN.</div></div></div><label class="fn3-master"><span class="fn3-switch"><input id="fn3AutoEnabled" type="checkbox"><span></span></span></label></div>
            <div class="fn3-info">${svg('info')}<span>Быстрый watchdog проверяет только живучесть текущего VPN. Тяжёлый подбор серверов запускается лишь после подтверждённого отказа.<br>Режим работы определяет, может ли автоматика только обновлять текущий профиль или также подбирать проверенную замену.</span></div>
            <div class="fn3-endpoint-schedule fn3-health-schedule">
              <div class="fn3-endpoint-copy"><strong>Проверка доступности VPN</strong><small>30 секунд — максимально быстро; 1 минута — рекомендуемый баланс; 5 минут — минимальная нагрузка.</small></div>
              <select id="fn3HealthInterval" aria-label="Интервал проверки доступности VPN">${healthIntervalOptions('1m')}</select>
            </div>
            <div class="fn3-section-label">Режим работы</div>
            <div class="fn3-mode-list">
              <label class="fn3-mode" data-mode-card="endpoint"><input type="radio" name="fn3Mode" value="endpoint"><span><strong>Только текущий VPN <em class="fn3-mode-badge">Минимум изменений</em></strong><small>Страна и VPN-профиль фиксированы. FreeNet обновляет только endpoint этого же профиля и никогда сам не переключается на другой.</small></span></label>
              <label class="fn3-mode" data-mode-card="best"><input type="radio" name="fn3Mode" value="best"><span><strong>Полный AUTO VPN <em class="fn3-mode-badge">Автовосстановление</em></strong><small>Восстанавливает отказавший VPN, а при повторяющейся деградации качества сравнивает проверенные варианты и может перейти на существенно лучший.</small></span></label>
            </div>
            <div id="fn3EndpointSchedule" class="fn3-endpoint-schedule" hidden>
              <div class="fn3-endpoint-copy"><strong>Плановое обновление endpoint</strong><small>Только для текущего логического VPN; неоднозначный или небезопасный результат ничего не меняет.</small></div>
              <select id="fn3EndpointInterval" aria-label="Интервал обновления endpoint">${intervalOptions('1h')}</select>
            </div>
            <div id="fn3ReplacementSettings" class="fn3-replacement">
              <div class="fn3-section-label">Где искать замену VPN</div>
              <div class="fn3-scope-list">
                <label class="fn3-scope" data-scope-card="current"><input type="radio" name="fn3Scope" value="current"><span><strong>Текущая страна</strong><small>Замена только в той же стране — другой сервер или город.</small></span><span class="fn3-chevron">${svg('chevron')}</span></label>
                <label class="fn3-scope" data-scope-card="region"><input type="radio" name="fn3Scope" value="region"><span><strong>Лучший отклик <em class="fn3-recommended">Рекомендуется</em></strong><small>FreeNet учитывает скорость, отклик и стабильность; после повторяющейся деградации или подтверждённого отказа применяет только полностью измеренный лучший вариант.</small></span><span class="fn3-chevron">${svg('chevron')}</span></label>
                <label class="fn3-scope" data-scope-card="allowlist"><input type="radio" name="fn3Scope" value="allowlist"><span><strong>Выбранные страны</strong><small>Вы сами выбираете список стран.</small></span><span class="fn3-chevron">${svg('chevron')}</span></label>
              </div>
            </div>
            <div class="fn3-safety">${svg('shield')}<span id="fn3SafetyText">Рабочий VPN не меняется без причины. Все переключения выполняются только при подтверждённом сбое, с проверкой и автоматическим возвратом при необходимости.</span></div>
            <div class="fn3-auto-actions"><button id="fn3Check" class="btn primary" type="button">${svg('play')}<span>Проверить сейчас</span></button><div class="fn3-control"><span class="fn3-control-dot"></span><div><strong id="fn3ControlTitle">Автопроверка</strong><small id="fn3ControlNext">Следующая проверка: —</small></div></div></div>
            <div class="fn3-journal-linkrow"><button id="fn3AllEvents" class="fn3-link" type="button">История проверок и переключений → Журнал</button></div>
          </section>
        </div>
        <section class="fn3-card fn3-extra"><div class="fn3-extra-title"><h2>Системное обслуживание</h2><div class="fn3-sub">Обновления, служебные данные и резервные копии.</div></div><div class="fn3-extra-grid">${extraCard('subscription','subscription','Обновление подписки','Автоматическое обновление данных подписки — списка доступных VPN.','Проверять','Проверить сейчас')}${extraCard('geodata','globe','GeoData / GeoIP','Данные геолокации, используемые для маршрутизации и фильтров.','Обновлять','Обновить сейчас')}${extraCard('freenet','box','Обновление FreeNet','Автоматическая проверка новых версий FreeNet. Установка — только после подтверждения.','Проверять','Проверить сейчас')}${backupCard()}</div><div class="fn3-extra-save-row"><button id="fn3MaintenanceSave" class="btn fn3-extra-save" type="button" disabled>${svg('save')}<span>Сохранено</span></button></div></section>
      </div>
      <div class="fn3-compat"><select id="fnDNS"><option value="firmware">Прямой</option><option value="xkeen">Раздельный</option></select><input id="fnAutoEnabled" type="checkbox"><select id="fnAutoInterval"><option value="manual">Вручную</option></select><input id="fnAutoApply" type="checkbox"><input type="radio" name="fnAutoMode" value="best"><input type="radio" name="fnAutoPolicy" value="degraded"><input type="radio" name="fnCountryScope" value="region"><input id="fnGeoDataEnabled" type="checkbox"><span id="fnGeoDataSchedule"></span><span id="fnAutoEnabledLabel"></span><span id="fnCurrentProfile"></span><span id="fnCurrentProfileSmall"></span><span id="fnCurrentEndpoint"></span><span id="fnCurrentFlag"></span><span id="fnWatchState"></span><span id="fnLastRun"></span><span id="fnLatency"></span><span id="fnSpeed"></span><span id="fnJitter"></span><span id="fnHealthBanner"></span><tbody id="fnJournalBody"></tbody><button id="fnSaveSettings"></button><button id="fnCheckNow"></button></div>
      <div id="fn3CountryPop" class="fn3-country-pop" hidden></div>`;
  }

  function intervalOptions(selected) {
    return ['30m','1h','3h','6h','12h','24h'].map(v => `<option value="${v}"${v===selected?' selected':''}>раз в ${intervalLabel(v)}</option>`).join('');
  }

  function extraCard(key, icon, title, description, verb, actionLabel) {
    return `<article class="fn3-extra-card"><div class="fn3-extra-head"><span class="fn3-extra-icon">${svg(icon)}</span><div><h3>${title}</h3><p>${description}</p></div><label class="fn3-switch"><input id="fn3_${key}_enabled" type="checkbox"><span></span></label></div><div class="fn3-extra-row"><label for="fn3_${key}_interval">${verb}</label><select id="fn3_${key}_interval"></select></div><div class="fn3-extra-meta"><span>Последняя проверка</span><b id="fn3_${key}_last">—</b><span>Следующая проверка</span><b id="fn3_${key}_next">—</b></div><button class="btn secondary fn3-extra-action" type="button" data-v3-action="${key}">${svg('refresh')}${actionLabel}</button></article>`;
  }

  function backupCard() {
    return `<article class="fn3-extra-card"><div class="fn3-extra-head"><span class="fn3-extra-icon">${svg('backup')}</span><div><h3>Резервное копирование</h3><p>Снимки хранятся локально на роутере и используются для безопасного восстановления.</p></div><label class="fn3-switch"><input id="fn3_backup_enabled" type="checkbox"><span></span></label></div><div class="fn3-extra-row"><label for="fn3_backup_interval">Создавать</label><select id="fn3_backup_interval"></select></div><div class="fn3-extra-meta"><span>Последняя копия</span><b id="fn3_backup_last">—</b><span>Следующая копия</span><b id="fn3_backup_next">—</b></div><div class="fn3-backup-storage"><span>Хранилище</span><code id="fn3_backup_root">—</code><span>Последний снимок</span><code id="fn3_backup_snapshot">пока нет</code><span>Полный путь</span><code id="fn3_backup_path">—</code></div><div class="fn3-backup-contents">Снимок фиксирует состояние настроек FreeNet, подписки, фильтра профилей и Xray outbounds. Секретные значения в браузере не показываются.</div><div id="fn3_backup_result" class="fn3-backup-result" hidden></div><div class="fn3-backup-actions"><button class="btn secondary" type="button" data-v3-action="backup_create" title="Создать внутренний снимок настроек на роутере">${svg('download')}Создать снимок</button><button class="btn secondary fn3-danger" type="button" data-v3-action="backup_restore" title="Восстановить последний внутренний снимок настроек на роутере">${svg('upload')}Восстановить последний</button></div></article>`;
  }

  function currentForm() {
    const mode = q('input[name="fn3Mode"]:checked')?.value === 'endpoint' ? 'endpoint' : 'best';
    const healthInterval = q('#fn3HealthInterval')?.value || '1m';
    const endpointInterval = q('#fn3EndpointInterval')?.value || '1h';
    const scope = q('input[name="fn3Scope"]:checked')?.value || 'region';
    const read = key => ({enabled: !!q(`#fn3_${key}_enabled`)?.checked, interval: q(`#fn3_${key}_interval`)?.value || ''});
    return {enabled: !!q('#fn3AutoEnabled')?.checked, mode, healthInterval, endpointInterval, scope, countries: state.countries.slice().sort(), subscription:read('subscription'), geodata:read('geodata'), freenet:read('freenet'), backup:read('backup')};
  }

  function formKey() { return JSON.stringify(currentForm()); }
  function markDirty() { state.dirty = formKey() !== state.baseline; renderSave(); renderMode(); }
  function renderSave() {
    const buttons = [q('#fn3Save'), q('#fn3MaintenanceSave')].filter(Boolean);
    if (!buttons.length) return;
    buttons.forEach(btn => {
      btn.disabled = state.saving || state.controlsBusy || !state.dirty;
      btn.classList.toggle('primary', state.dirty);
      const label = q('span', btn);
      if (label) label.textContent = state.saving ? 'Сохраняем…' : state.dirty ? 'Сохранить изменения' : 'Сохранено';
    });
  }
  function renderScope() { qa('[data-scope-card]').forEach(n => n.classList.toggle('selected', q('input',n)?.checked)); }
  function renderMode() {
    const mode = q('input[name="fn3Mode"]:checked')?.value === 'endpoint' ? 'endpoint' : 'best';
    const endpointOnly = mode === 'endpoint';
    qa('[data-mode-card]').forEach(n => n.classList.toggle('selected', q('input', n)?.checked));
    const schedule = q('#fn3EndpointSchedule');
    const replacement = q('#fn3ReplacementSettings');
    if (schedule) schedule.hidden = !endpointOnly;
    if (replacement) replacement.hidden = endpointOnly;
    qa('input[name="fn3Scope"]').forEach(input => { input.disabled = endpointOnly; });
    renderScope();

    const safety = q('#fn3SafetyText');
    if (safety) safety.textContent = endpointOnly
      ? 'Страна и VPN-профиль зафиксированы. FreeNet может изменить только endpoint текущего профиля; если безопасное восстановление не подтверждено — STOP без смены сервера.'
      : 'Рабочий VPN не меняется без причины. При подтверждённом сбое FreeNet сначала восстанавливает текущий endpoint и только затем может применить полностью проверенную замену.';

    const enabled = !!q('#fn3AutoEnabled')?.checked;
    const title = q('#fn3ControlTitle');
    const next = q('#fn3ControlNext');
    if (!enabled) {
      if (title) title.textContent = 'Автоматика выключена';
      if (next) next.textContent = 'Автоматические проверки не выполняются';
    } else if (endpointOnly) {
      if (title) title.textContent = 'Только текущий VPN';
      if (next) next.textContent = `Контроль: каждые ${healthIntervalLabel(q('#fn3HealthInterval')?.value || '1m')} · endpoint: раз в ${intervalLabel(q('#fn3EndpointInterval')?.value || '1h')}`;
    } else {
      if (title) title.textContent = 'Полный AUTO VPN';
      if (next) next.textContent = `Следующая проверка: ${nextLabel(state.data?.auto_vpn?.next_health)} · интервал ${healthIntervalLabel(q('#fn3HealthInterval')?.value || '1m')}`;
    }
  }

  async function fetchJSON(url, options) {
    const res = await fetch(url, options);
    const type = res.headers.get('content-type') || '';
    const data = type.includes('application/json') ? await res.json() : {success:false,error:`HTTP ${res.status}`};
    if (!res.ok || data.success === false) throw new Error(data.error || `HTTP ${res.status}`);
    return data;
  }

  function applySchedule(key, data) {
    const enabled = q(`#fn3_${key}_enabled`), interval = q(`#fn3_${key}_interval`);
    if (enabled) enabled.checked = !!data?.enabled;
    if (interval) { interval.innerHTML = intervalOptions(data?.interval || ({subscription:'6h',geodata:'3h',freenet:'12h',backup:'24h'})[key]); interval.value = data?.interval || ({subscription:'6h',geodata:'3h',freenet:'12h',backup:'24h'})[key]; }
    const last = q(`#fn3_${key}_last`), next = q(`#fn3_${key}_next`);
    if (last) {
      const success = key === 'backup' ? 'Снимок создан' : 'Успешно';
      last.textContent = data?.last_run ? `${formatDate(data.last_run)}${data.result ? ` · ${data.result === 'success' ? success : data.result === 'available' ? 'Доступно обновление' : 'Ошибка'}` : ''}` : '—';
    }
    if (last) last.classList.toggle('ok', data?.result === 'success' || data?.result === 'available');
    if (next) next.textContent = data?.enabled ? (data?.next_run ? formatDate(data.next_run) : 'после первого запуска') : 'выключено';
  }

  function renderBackupInfo(info) {
    const root = q('#fn3_backup_root');
    const snapshot = q('#fn3_backup_snapshot');
    const path = q('#fn3_backup_path');
    if (root) root.textContent = info?.root || '—';
    if (snapshot) snapshot.textContent = info?.latest || 'пока нет';
    if (path) path.textContent = info?.latest_path || '—';
    const restore = q('[data-v3-action="backup_restore"]');
    if (restore) {
      restore.disabled = !info?.latest;
      restore.title = info?.latest
        ? `Восстановить снимок ${info.latest}`
        : 'Сначала создайте снимок настроек FreeNet';
    }
  }

  function showBackupActionResult(data, action) {
    const host = q('#fn3_backup_result'); if (!host) return;
    const info = data?.backup_info || {};
    host.hidden = false;
    host.classList.remove('bad');
    host.textContent = '';
    const title = document.createElement('strong');
    title.textContent = action === 'backup_restore' ? 'Снимок восстановлен и проверен' : 'Снимок создан';
    host.appendChild(title);
    if (info.latest) {
      const name = document.createElement('span'); name.textContent = info.latest; host.appendChild(name);
    }
    if (info.latest_path) {
      const path = document.createElement('code'); path.textContent = info.latest_path; host.appendChild(path);
    }
    const note = document.createElement('span');
    note.textContent = action === 'backup_restore'
      ? 'FreeNet применил snapshot и проверил восстановленные файлы. При ошибке используется отдельный rollback snapshot.'
      : `Зафиксировано состояние ${info.tracked || 4} контролируемых компонентов и manifest. Этот snapshot теперь используется действием «Восстановить последний».`;
    host.appendChild(note);
  }

  function showBackupActionError(message) {
    const host = q('#fn3_backup_result'); if (!host) return;
    host.hidden = false;
    host.classList.add('bad');
    host.textContent = '';
    const title = document.createElement('strong'); title.textContent = 'Операция не выполнена';
    const note = document.createElement('span'); note.textContent = message || 'Неизвестная ошибка';
    host.append(title, note);
  }

  function applyData(data) {
    state.data = data;
    const auto = data.auto_vpn || {};
    q('#fn3AutoEnabled').checked = !!auto.enabled;
    q('#fn3AutoLabel').textContent = auto.enabled ? 'Включено' : 'Выключено';
    const mode = auto.mode === 'endpoint' ? 'endpoint' : 'best';
    const modeInput = q(`input[name="fn3Mode"][value="${mode}"]`); if (modeInput) modeInput.checked = true;
    const healthInterval = ['30s','1m','5m'].includes(auto.health_interval) ? auto.health_interval : '1m';
    const healthSelect = q('#fn3HealthInterval');
    if (healthSelect) { healthSelect.innerHTML = healthIntervalOptions(healthInterval); healthSelect.value = healthInterval; }
    const endpointInterval = ['30m','1h','3h','6h','12h','24h'].includes(auto.endpoint_interval) ? auto.endpoint_interval : '1h';
    const endpointSelect = q('#fn3EndpointInterval');
    if (endpointSelect) { endpointSelect.innerHTML = intervalOptions(endpointInterval); endpointSelect.value = endpointInterval; }
    state.countries = Array.isArray(auto.countries) ? auto.countries.slice() : [];
    const scope = ['current','region','allowlist'].includes(auto.country_scope) ? auto.country_scope : 'region';
    const scopeInput = q(`input[name="fn3Scope"][value="${scope}"]`); if (scopeInput) scopeInput.checked = true;
    renderMode();

    if (!state.journalEvents.length && Array.isArray(data.events)) state.journalEvents = data.events.slice();
    if (journalPageActive()) renderJournalPageState();
    applySchedule('subscription', data.subscription); applySchedule('geodata', data.geodata); applySchedule('freenet', data.freenet); applySchedule('backup', data.backup);
    renderBackupInfo(data.backup_info);
    state.baseline = formKey(); state.dirty = false; renderSave();
    window.__freenetSettingsV3Snapshot = data;
    document.dispatchEvent(new CustomEvent('freenet:settings-v3-updated', {detail:data}));
  }

  function journalCategory(event) {
    const kind = String(event?.kind || '').trim().toLowerCase();
    if (kind === 'vpn') return 'vpn';
    if (kind === 'auto vpn' || kind === 'auto_vpn') return 'auto';
    if (kind === 'subscription') return 'subscription';
    return 'system';
  }

  function journalStage(event) {
    if (journalCategory(event) !== 'auto') return '';
    const raw = String(event?.result || '').trim().toLowerCase();
    if (raw === 'selection') return 'selection';
    const split = raw.indexOf(':');
    return split > 0 ? raw.slice(0, split) : '';
  }

  function journalStageLabel(stage) {
    return ({
      detect:'Проверка',
      wan:'Обычный интернет',
      confirm:'Подтверждение',
      endpoint_refresh:'Endpoint',
      candidate_selection:'Подбор',
      selection:'Решение',
      apply:'Применение',
      post_check:'Post-check',
      rollback:'Rollback',
      post_update_guard:'После обновления',
      rollback_guard:'Защита rollback'
    })[stage] || '';
  }

  function journalKind(event) {
    const kind = String(event?.kind || '').trim();
    const category = journalCategory(event);
    if (category === 'vpn') return ['VPN', 'vpn'];
    if (category === 'auto') {
      const stage = journalStageLabel(journalStage(event));
      return [stage ? `AUTO VPN · ${stage}` : 'AUTO VPN', 'auto'];
    }
    if (category === 'subscription') return ['Подписка', 'system'];
    if (kind === 'geodata') return ['GeoData / GeoIP', 'system'];
    if (kind === 'freenet') return ['FreeNet', 'system'];
    if (kind === 'freenet_release_catalog') return ['Каталог FreeNet', 'system'];
    if (kind === 'freenet_update' || kind === 'freenet_update_recovery') return ['Обновление FreeNet', 'system'];
    if (kind === 'backup') return ['Резервная копия', 'system'];
    return [kind || 'Система', 'system'];
  }

  function journalTone(event) {
    return humanResult(event?.result, event?.message)[2];
  }

  function normalizedSearch(value) {
    return String(value || '').toLocaleLowerCase('ru-RU').replace(/\s+/g, ' ').trim();
  }

  function filteredJournalEvents(events) {
    let rows = Array.isArray(events) ? events.slice() : [];
    if (state.journalFilter !== 'all') rows = rows.filter(event => journalCategory(event) === state.journalFilter);
    if (state.journalResultFilter !== 'all') rows = rows.filter(event => journalTone(event) === state.journalResultFilter);
    const query = normalizedSearch(state.journalQuery);
    if (query) {
      rows = rows.filter(event => {
        const [result, translated] = humanResult(event?.result, event?.message);
        const [kind] = journalKind(event);
        const haystack = normalizedSearch([
          formatDate(event?.at), event?.at, event?.kind, event?.result, kind, result, translated, event?.message
        ].join(' '));
        return haystack.includes(query);
      });
    }
    return rows;
  }

  function renderJournal(events, target = '#fn3JournalFull') {
    const body = q(target); if (!body) return;
    const source = Array.isArray(events) ? events : [];
    const filtered = source.slice(0, target === '#fn3JournalFull' ? state.journalPageSize : 4);
    if (!filtered.length) {
      body.innerHTML = '<div class="fn3-journal-empty">По текущему поиску и фильтрам событий нет.</div>';
      return;
    }
    body.innerHTML = filtered.map(e => {
      const [result, msg, tone] = humanResult(e.result, e.message);
      const [kind, kindClass] = journalKind(e);
      const toneClass = tone === 'ok' ? 'ok' : tone === 'bad' ? 'bad' : 'neutral';
      const resultClass = tone === 'ok' ? '' : tone === 'bad' ? 'bad' : 'neutral';
      return `<article class="fn3-journal-event ${toneClass}">
        <div class="fn3-journal-event-rail"><span class="fn3-journal-event-dot"></span></div>
        <div class="fn3-journal-event-main">
          <div class="fn3-journal-event-top">
            <div class="fn3-journal-event-tags"><span class="fn3-kind ${kindClass}">${escapeHTML(kind)}</span><span class="fn3-result ${resultClass}"><i class="fn3-dot"></i>${escapeHTML(result)}</span></div>
            <time class="fn3-journal-event-time">${formatDate(e.at)}</time>
          </div>
          <div class="fn3-journal-event-message">${escapeHTML(msg)}</div>
        </div>
      </article>`;
    }).join('');
  }

  function escapeHTML(value) { return String(value || '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

  async function load() {
    try {
      const data = await fetchJSON('/api/settings-v3', {cache:'no-store'});
      applyData(data);
    } catch (err) {
      console.error('Settings v3 load failed', err);
    }
  }

  async function save() {
    if (!state.dirty || state.saving) return;
    state.saving = true; renderSave();
    const form = currentForm();
    try {
      const data = await fetchJSON('/api/settings-v3', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'save',auto_vpn_enabled:form.enabled,auto_vpn_mode:form.mode,auto_vpn_health_interval:form.healthInterval,auto_vpn_endpoint_interval:form.endpointInterval,country_scope:form.scope,countries:form.countries,subscription_enabled:form.subscription.enabled,subscription_interval:form.subscription.interval,geodata_enabled:form.geodata.enabled,geodata_interval:form.geodata.interval,freenet_enabled:form.freenet.enabled,freenet_interval:form.freenet.interval,backup_enabled:form.backup.enabled,backup_interval:form.backup.interval})});
      applyData(data);
    } catch (err) { alert(`Не удалось сохранить настройки: ${err.message}`); }
    finally { state.saving = false; renderSave(); }
  }

  async function action(name) {
    const actionMap = {subscription:'subscription_check',geodata:'geodata_update',freenet:'freenet_check'};
    const action = actionMap[name] || name;
    try {
      if (action === 'backup_restore') {
        const latest = state.data?.backup_info?.latest || '';
        const target = latest ? `снимок ${latest}` : 'последний снимок настроек FreeNet';
        if (!confirm(`Восстановить ${target}? Перед восстановлением FreeNet создаст отдельный аварийный snapshot текущих файлов.`)) return;
      }
      const data = await fetchJSON('/api/settings-v3/action', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action})});
      if (action === 'backup_create' || action === 'backup_restore') {
        showBackupActionResult(data, action);
        if (data.backup_info) renderBackupInfo(data.backup_info);
      } else if (data.message) {
        console.info(data.message);
      }
      await load();
    } catch (err) {
      if (action === 'backup_create' || action === 'backup_restore') showBackupActionError(err.message);
      else alert(`Операция не выполнена: ${err.message}`);
    }
  }

  async function checkAuto() {
    if (state.checking) return;
    state.checking = true;
    const btn = q('#fn3Check'); const started = Date.now();
    try {
      await fetchJSON('/api/automation', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'check'})});
      for (;;) {
        const s = await fetchJSON('/api/automation/check', {cache:'no-store'});
        const sec = Math.max(1, Math.floor((Date.now()-started)/1000));
        if (btn) q('span',btn).textContent = `Проверяем… ${sec} с`;
        if (!s.active) break;
        await new Promise(r => setTimeout(r, 1200));
      }
      await load();
    } catch (err) { alert(`Проверка не завершена: ${err.message}`); }
    finally { state.checking = false; if (btn) q('span',btn).textContent = 'Проверить сейчас'; }
  }

  function countryPopupHeader() {
    return `<div style="display:flex;justify-content:space-between;align-items:center"><strong>Выбранные страны</strong><button type="button" id="fn3PopClose" class="fn3-link">Закрыть</button></div>`;
  }

  function positionCountryPopup(pop) {
    const selected = q('[data-scope-card="allowlist"]'); const r = selected?.getBoundingClientRect();
    pop.style.left = `${Math.min(window.innerWidth-540, Math.max(14, r?.left || 100))}px`;
    pop.style.top = `${Math.min(window.innerHeight-630, Math.max(14, (r?.bottom || 100)+6))}px`;
  }

  function bindCountryPopupActions() {
    const pop = q('#fn3CountryPop'); if (!pop) return;
    const close = q('#fn3PopClose', pop); if (close) close.onclick = () => pop.hidden = true;
    const apply = q('#fn3CountriesApply', pop);
    if (apply) apply.onclick = () => {
      state.countries = qa('.fn3-country-item input:checked',pop).map(x=>x.value);
      pop.hidden = true;
      markDirty();
    };
  }

  function renderCountryPopup() {
    const pop = q('#fn3CountryPop'); if (!pop) return;
    if (state.countryCatalogLoading && !state.countryCatalog.length) {
      pop.innerHTML = `${countryPopupHeader()}<div class="fn3-country-state">Получаем доступные страны из текущей подписки…</div>`;
      bindCountryPopupActions();
      return;
    }

    const rows = state.countryCatalog.map(option => {
      const code = String(option?.code || '').toLowerCase();
      if (!/^[a-z]{2}$/.test(code) || code === 'ru') return '';
      const checked = state.countries.includes(code) ? ' checked' : '';
      const unavailable = option.available === false;
      const note = unavailable ? '<small>Ранее выбрана — сейчас нет активного Extra-профиля</small>' : '';
      return `<label class="fn3-country-item${unavailable?' unavailable':''}"><input type="checkbox" value="${code}"${checked}>${countryFlagMarkup(code)}<span class="fn3-country-copy"><span>${escapeHTML(option.name || 'Страна VPN')}</span>${note}</span></label>`;
    }).filter(Boolean).join('');
    const warning = state.countryCatalogWarning ? `<div class="fn3-country-state">${escapeHTML(state.countryCatalogWarning)}</div>` : '';
    const empty = rows ? '' : '<div class="fn3-country-state">Доступные страны пока не найдены. Текущий сохранённый выбор не меняется.</div>';
    pop.innerHTML = `${countryPopupHeader()}${warning}${rows?`<div class="fn3-country-list">${rows}</div>`:''}${empty}<div class="fn3-pop-actions"><button id="fn3CountriesApply" class="btn primary" type="button"${rows?'':' disabled'}>Применить список</button></div>`;
    bindCountryPopupActions();
  }

  async function loadCountryCatalog(force = false) {
    if (state.countryCatalogLoading) return;
    if (state.countryCatalog.length && !force) return;
    state.countryCatalogLoading = true;
    state.countryCatalogWarning = '';
    renderCountryPopup();
    try {
      const data = await fetchJSON('/api/settings-v3/countries', {cache:'no-store'});
      state.countryCatalog = Array.isArray(data.countries) ? data.countries : [];
      state.countryCatalogFresh = !!data.fresh;
      state.countryCatalogWarning = data.warning || '';
      if (!state.countries.length && Array.isArray(data.selected)) state.countries = data.selected.slice();
    } catch (_) {
      state.countryCatalogWarning = 'Не удалось обновить каталог стран. Сохранённый выбор не изменён.';
    } finally {
      state.countryCatalogLoading = false;
      renderCountryPopup();
    }
  }

  function openCountries() {
    const pop = q('#fn3CountryPop'); if (!pop) return;
    pop.hidden = false;
    positionCountryPopup(pop);
    renderCountryPopup();
    void loadCountryCatalog(true);
  }

  function bind() {
    q('#fn3Save').onclick = save; q('#fn3MaintenanceSave').onclick = save; q('#fn3Check').onclick = checkAuto;
    document.addEventListener('freenet:controls-busy', event => {
      state.controlsBusy = !!event.detail;
      queueMicrotask(renderSave);
    });
    q('#fn3AutoEnabled').onchange = () => { q('#fn3AutoLabel').textContent = q('#fn3AutoEnabled').checked ? 'Включено' : 'Выключено'; markDirty(); };
    qa('input[name="fn3Mode"]').forEach(i => i.onchange = markDirty);
    q('#fn3HealthInterval').onchange = markDirty;
    q('#fn3EndpointInterval').onchange = markDirty;
    qa('input[name="fn3Scope"]').forEach(i => i.onchange = () => { if (i.value === 'allowlist') openCountries(); markDirty(); });
    ['subscription','geodata','freenet','backup'].forEach(k => { q(`#fn3_${k}_enabled`).onchange = markDirty; q(`#fn3_${k}_interval`).onchange = markDirty; });
    qa('[data-v3-action]').forEach(b => b.onclick = () => action(b.dataset.v3Action));
    q('#fn3AllEvents').onclick = () => window.openFreeNetJournal('all');
    q('[data-scope-card="allowlist"]').ondblclick = openCountries;
  }

  function ensureSettingsPage() {
    let page = q('[data-page-view="settings"]');
    if (page) return page;
    const content = q('.content');
    if (!content) return null;
    page = document.createElement('section');
    page.className = 'page fn3-page';
    page.dataset.pageView = 'settings';
    content.appendChild(page);
    return page;
  }

  function mountSettings() {
    const page = ensureSettingsPage();
    if (!page) return false;
    injectStyles(); rewireNavigation();
    if (page.dataset.settingsV3 === '1') return true;
    page.dataset.settingsV3 = '1';
    page.classList.add('fn3-page');
    page.innerHTML = pageMarkup();
    bind(); load();
    return true;
  }

  function journalPageActive() {
    const page = q('[data-page-view="journal"]');
    return !!page && document.visibilityState !== 'hidden' &&
      (page.classList.contains('active') || location.hash.slice(1) === 'journal');
  }

  function journalStatsFallback(events) {
    const list = Array.isArray(events) ? events : [];
    const stats = {total:list.length,success:0,neutral:0,errors:0};
    list.forEach(event => {
      const cls = humanResult(event?.result, event?.message)[2];
      if (cls === 'ok') stats.success += 1;
      else if (cls === 'bad') stats.errors += 1;
      else stats.neutral += 1;
    });
    return stats;
  }

  function renderJournalSummary() {
    const host = q('#fn3JournalSummary'); if (!host) return;
    const stats = state.journalStats || journalStatsFallback(state.journalEvents);
    host.innerHTML = `
      <div class="fn3-journal-stat"><span>Событий в выборке</span><strong>${Number(stats.total || state.journalFilteredTotal || 0)}</strong></div>
      <div class="fn3-journal-stat ok"><span>Успешно</span><strong>${Number(stats.success || 0)}</strong></div>
      <div class="fn3-journal-stat"><span>Служебные / без изменений</span><strong>${Number(stats.neutral || 0)}</strong></div>
      <div class="fn3-journal-stat bad"><span>Ошибки</span><strong>${Number(stats.errors || 0)}</strong></div>`;
  }

  function journalRangeParams() {
    const now = new Date();
    let from = '', to = '';
    if (state.journalDatePreset === '24h') {
      from = new Date(now.getTime() - 24 * 60 * 60 * 1000).toISOString();
      to = now.toISOString();
    } else if (state.journalDatePreset === '7d') {
      from = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000).toISOString();
      to = now.toISOString();
    } else if (state.journalDatePreset === 'today') {
      const start = new Date(now.getFullYear(), now.getMonth(), now.getDate());
      from = start.toISOString();
      to = now.toISOString();
    } else if (state.journalDatePreset === 'custom') {
      if (state.journalCustomFrom) from = new Date(state.journalCustomFrom).toISOString();
      if (state.journalCustomTo) to = new Date(state.journalCustomTo).toISOString();
    }
    return {from,to};
  }

  function journalQueryString(includePage = true) {
    const params = new URLSearchParams();
    if (includePage) {
      params.set('page', String(state.journalPage || 1));
      params.set('page_size', String(state.journalPageSize || 100));
    }
    if (state.journalFilter !== 'all') params.set('category', state.journalFilter);
    if (state.journalResultFilter !== 'all') params.set('result', state.journalResultFilter);
    if (state.journalQuery) params.set('q', state.journalQuery);
    const range = journalRangeParams();
    if (range.from) params.set('from', range.from);
    if (range.to) params.set('to', range.to);
    return params.toString();
  }

  function renderJournalPageState() {
    const page = q('[data-page-view="journal"]'); if (!page || page.dataset.journalV4 !== '1') return;
    qa('[data-journal-filter]', page).forEach(button => button.classList.toggle('active', button.dataset.journalFilter === state.journalFilter));
    qa('[data-journal-result]', page).forEach(button => button.classList.toggle('active', button.dataset.journalResult === state.journalResultFilter));
    qa('[data-journal-range]', page).forEach(button => button.classList.toggle('active', button.dataset.journalRange === state.journalDatePreset));
    const custom = q('#fn3JournalCustomRange', page);
    if (custom) custom.hidden = state.journalDatePreset !== 'custom';
    const search = q('#fn3JournalSearch', page);
    if (search && document.activeElement !== search && search.value !== state.journalQuery) search.value = state.journalQuery;
    const advanced = q('#fn3JournalAdvancedFilters', page);
    if (advanced) advanced.hidden = !state.journalFiltersOpen;
    const activeFilterCount = [
      state.journalDatePreset !== '24h',
      state.journalFilter !== 'all',
      state.journalResultFilter !== 'all'
    ].filter(Boolean).length;
    const filterToggle = q('#fn3JournalFiltersToggle', page);
    if (filterToggle) {
      filterToggle.classList.toggle('active', state.journalFiltersOpen || activeFilterCount > 0);
      filterToggle.setAttribute('aria-expanded', state.journalFiltersOpen ? 'true' : 'false');
      filterToggle.innerHTML = `Фильтры${activeFilterCount ? `<span class="fn3-journal-filter-count">${activeFilterCount}</span>` : ''}`;
    }
    const live = q('#fn3JournalLive', page);
    if (live) {
      live.classList.toggle('active', state.journalLive);
      live.setAttribute('aria-pressed', state.journalLive ? 'true' : 'false');
      live.setAttribute('title', state.journalLive ? 'Остановить автообновление журнала' : 'Возобновить автообновление журнала');
      live.innerHTML = state.journalLive
        ? '<span class="fn3-journal-live-dot"></span>Пауза'
        : 'Возобновить';
    }
    const meta = q('#fn3JournalMeta', page);
    if (meta) {
      meta.classList.toggle('bad', !!state.journalError);
      const retained = state.journalRetainedFrom && state.journalRetainedTo
        ? ` · архив ${formatDate(state.journalRetainedFrom)} → ${formatDate(state.journalRetainedTo)}`
        : '';
      meta.textContent = state.journalError
        ? `Не удалось обновить журнал: ${state.journalError}`
        : state.journalRefreshing
          ? 'Обновляем read-only журнал…'
          : state.journalGeneratedAt
            ? `Сохранено ${state.journalTotal} событий · выбрано ${state.journalFilteredTotal}${retained} · read-only`
            : 'Read-only журнал готов к обновлению.';
    }
    const retention = q('#fn3JournalRetentionRange', page);
    if (retention) {
      retention.textContent = state.journalRetainedFrom && state.journalRetainedTo
        ? `Архив: ${formatDate(state.journalRetainedFrom)} → ${formatDate(state.journalRetainedTo)}`
        : 'Архив: данных пока нет';
    }
    const pageLabel = q('#fn3JournalPageLabel', page);
    if (pageLabel) pageLabel.textContent = `Страница ${state.journalPage} из ${state.journalPages}`;
    const prev = q('#fn3JournalPrev', page); if (prev) prev.disabled = state.journalPage <= 1;
    const next = q('#fn3JournalNext', page); if (next) next.disabled = state.journalPage >= state.journalPages;
    const size = q('#fn3JournalPageSize', page); if (size) size.value = String(state.journalPageSize);
    renderJournalSummary();
    renderJournal(state.journalEvents, '#fn3JournalFull');
  }

  async function loadJournal(force = false) {
    if (state.journalRefreshing) return;
    if (!force && (!state.journalLive || !journalPageActive())) return;
    if (document.visibilityState === 'hidden') return;
    state.journalRefreshing = true;
    state.journalError = '';
    renderJournalPageState();
    try {
      const data = await fetchJSON('/api/journal?' + journalQueryString(true), {cache:'no-store'});
      const rawEvents = Array.isArray(data.events) ? data.events : [];
      const legacyEventOnlyPayload = data.total == null && data.filtered_total == null && data.stats == null;
      state.journalEvents = legacyEventOnlyPayload ? filteredJournalEvents(rawEvents) : rawEvents;
      state.journalGeneratedAt = data.generated_at || new Date().toISOString();
      state.journalTotal = Number(data.total ?? rawEvents.length);
      state.journalFilteredTotal = Number(data.filtered_total ?? state.journalEvents.length);
      state.journalPage = Number(data.page || 1);
      state.journalPages = Number(data.pages || 1);
      state.journalPageSize = Number(data.page_size || state.journalPageSize || 100);
      state.journalStats = data.stats || journalStatsFallback(state.journalEvents);
      state.journalRetainedFrom = data.retained_from || '';
      state.journalRetainedTo = data.retained_to || '';
    } catch (err) {
      state.journalError = err?.message || 'неизвестная ошибка чтения';
    } finally {
      state.journalRefreshing = false;
      renderJournalPageState();
    }
  }

  function ensureJournalLiveTimer() {
    if (state.journalTimer) return;
    state.journalTimer = window.setInterval(() => {
      if (journalPageActive() && state.journalLive) void loadJournal(false);
    }, 5000);
  }

  function resetJournalPageAndLoad() {
    state.journalPage = 1;
    void loadJournal(true);
  }

  function mountJournalPage() {
    const page = q('[data-page-view="journal"]'); if (!page) return;
    state.journalFiltersOpen = false;
    const filters = [['all','Все'],['vpn','VPN'],['auto','AUTO VPN'],['subscription','Подписка'],['system','Система']];
    const results = [['all','Все результаты'],['ok','Успешно'],['neutral','Служебные / без изменений'],['bad','Ошибки']];
    const ranges = [['today','Сегодня'],['24h','24 часа'],['7d','7 дней'],['custom','Интервал']];
    if (page.dataset.journalV4 !== '1') {
      page.dataset.journalV4 = '1';
      page.innerHTML = `<section class="fn3-journal-hero">
        <div class="fn3-journal-kicker">СОБЫТИЯ И ДИАГНОСТИКА</div>
        <h1>Журнал</h1>
        <p>История значимых событий FreeNet: VPN, AUTO VPN, обновления и системные действия. Повторяющиеся штатные проверки агрегируются, а инциденты и этапы восстановления сохраняются подробно.</p>
        <div class="fn3-journal-policy">
          <span>До 15 000 значимых событий</span>
          <span class="healthy">Штатная отметка — не чаще 1 раза в 6 часов</span>
          <span id="fn3JournalRetentionRange">Архив загружается…</span>
        </div>
      </section>
      <section class="fn3-journal-control-card">
        <div class="fn3-journal-toolbar"><input id="fn3JournalSearch" class="fn3-journal-search" type="search" autocomplete="off" placeholder="Поиск: сервер, AUTO, ошибка, обновление, 189 мс…"><div class="fn3-journal-actions"><button id="fn3JournalFiltersToggle" class="fn3-journal-filter-toggle" type="button" aria-expanded="false">Фильтры</button><button id="fn3JournalExport" class="fn3-journal-export" type="button">Экспорт CSV</button><button id="fn3JournalLive" class="fn3-journal-live active" type="button" aria-pressed="true" title="Остановить автообновление журнала"><span class="fn3-journal-live-dot"></span>Пауза</button><button id="fn3JournalRefresh" class="fn3-journal-refresh" type="button">Обновить</button></div></div>
        <div id="fn3JournalMeta" class="fn3-journal-meta">Read-only журнал готов к обновлению.</div>
        <div id="fn3JournalAdvancedFilters" class="fn3-journal-filter-groups" hidden>
          <div class="fn3-journal-filter-row"><span class="fn3-journal-filter-title">Период</span><div class="fn3-journal-range">${ranges.map(([key,label]) => `<button class="fn3-journal-filter" type="button" data-journal-range="${key}">${label}</button>`).join('')}<span id="fn3JournalCustomRange" class="fn3-journal-custom" hidden><input id="fn3JournalFrom" type="datetime-local" aria-label="Начало периода"><input id="fn3JournalTo" type="datetime-local" aria-label="Конец периода"></span></div></div>
          <div class="fn3-journal-filter-row"><span class="fn3-journal-filter-title">Событие</span><div class="fn3-journal-filters">${filters.map(([key,label]) => `<button class="fn3-journal-filter" type="button" data-journal-filter="${key}">${label}</button>`).join('')}</div></div>
          <div class="fn3-journal-filter-row"><span class="fn3-journal-filter-title">Результат</span><div class="fn3-journal-filters">${results.map(([key,label]) => `<button class="fn3-journal-filter" type="button" data-journal-result="${key}">${label}</button>`).join('')}</div></div>
        </div>
      </section>
      <div id="fn3JournalSummary" class="fn3-journal-summary"></div>
      <section class="fn3-journal-stream-card">
        <div class="fn3-journal-stream-head"><div><h2>События</h2><p>Новое сверху · подробные этапы инцидента сохраняются без агрегации</p></div></div>
        <div id="fn3JournalFull" class="fn3-journal-event-list"></div>
        <div class="fn3-journal-pager"><span class="fn3-journal-retention">Исходная история автоматически ограничена 20 000 строками на файл; старые записи удаляются без ручной очистки.</span><div class="fn3-journal-pager-controls"><label class="fn3-journal-page-label">На странице <select id="fn3JournalPageSize" class="fn3-journal-page-size"><option>50</option><option selected>100</option><option>200</option><option>500</option></select></label><button id="fn3JournalPrev" class="fn3-journal-page-btn" type="button">← Назад</button><span id="fn3JournalPageLabel" class="fn3-journal-page-label">Страница 1 из 1</span><button id="fn3JournalNext" class="fn3-journal-page-btn" type="button">Вперёд →</button></div></div>
      </section>`;

      qa('[data-journal-filter]', page).forEach(button => button.onclick = () => {
        state.journalFilter = button.dataset.journalFilter || 'all';
        resetJournalPageAndLoad();
      });
      qa('[data-journal-result]', page).forEach(button => button.onclick = () => {
        state.journalResultFilter = button.dataset.journalResult || 'all';
        resetJournalPageAndLoad();
      });
      qa('[data-journal-range]', page).forEach(button => button.onclick = () => {
        state.journalDatePreset = button.dataset.journalRange || '24h';
        renderJournalPageState();
        if (state.journalDatePreset !== 'custom') resetJournalPageAndLoad();
      });
      const from = q('#fn3JournalFrom', page);
      const to = q('#fn3JournalTo', page);
      if (from) from.onchange = () => { state.journalCustomFrom = from.value || ''; if (state.journalCustomTo) resetJournalPageAndLoad(); };
      if (to) to.onchange = () => { state.journalCustomTo = to.value || ''; if (state.journalCustomFrom) resetJournalPageAndLoad(); };
      const search = q('#fn3JournalSearch', page);
      if (search) search.oninput = () => {
        state.journalQuery = search.value || '';
        window.clearTimeout(state.journalSearchTimer);
        state.journalSearchTimer = window.setTimeout(resetJournalPageAndLoad, 300);
      };
      const filtersToggle = q('#fn3JournalFiltersToggle', page);
      if (filtersToggle) filtersToggle.onclick = () => {
        state.journalFiltersOpen = !state.journalFiltersOpen;
        renderJournalPageState();
      };
      const live = q('#fn3JournalLive', page);
      if (live) live.onclick = () => {
        state.journalLive = !state.journalLive;
        renderJournalPageState();
        if (state.journalLive) void loadJournal(true);
      };
      const refresh = q('#fn3JournalRefresh', page);
      if (refresh) refresh.onclick = () => void loadJournal(true);
      const exportButton = q('#fn3JournalExport', page);
      if (exportButton) exportButton.onclick = () => {
        window.location.href = '/api/journal/export?' + journalQueryString(false);
      };
      const pageSize = q('#fn3JournalPageSize', page);
      if (pageSize) pageSize.onchange = () => {
        state.journalPageSize = Number(pageSize.value || 100);
        resetJournalPageAndLoad();
      };
      const prev = q('#fn3JournalPrev', page);
      if (prev) prev.onclick = () => { if (state.journalPage > 1) { state.journalPage -= 1; void loadJournal(true); } };
      const next = q('#fn3JournalNext', page);
      if (next) next.onclick = () => { if (state.journalPage < state.journalPages) { state.journalPage += 1; void loadJournal(true); } };
    }
    renderJournalPageState();
    ensureJournalLiveTimer();
    void loadJournal(true);
  }

  window.openFreeNetJournal = filter => {
    const allowed = new Set(['all','vpn','auto','subscription','system']);
    state.journalFilter = allowed.has(String(filter || '')) ? String(filter) : 'all';
    if (typeof window.setPage === 'function') window.setPage('journal');
    mountJournalPage();
  };

  function boot() {
    rewireNavigation();
    mountSettings();
    ensureJournalLiveTimer();
    document.addEventListener('visibilitychange', () => {
      if (journalPageActive() && state.journalLive) void loadJournal(false);
    });
    window.addEventListener('hashchange', () => {
      if (location.hash.slice(1) === 'journal') mountJournalPage();
    });
    if (location.hash.slice(1) === 'settings' && typeof window.setPage === 'function') window.setPage('settings');
    if (location.hash.slice(1) === 'journal') mountJournalPage();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot, {once:true});
  else boot();
})();
