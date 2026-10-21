// Runs in the existing shipped-page Playwright gate. Only HTTP fixtures and
// public DOM events are supplied; the renderer and bootstrap are production JS.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

module.exports = async function runCurrentQualityUIContract({browser, base, root}) {
  const identities = ['a', 'b', 'c', 'd'].map(letter => letter.repeat(64));
  const endpoint = '192.0.2.10:443';
  const times = [1, 2, 3, 4, 5, 6, 7, 8].map(minute => '2026-10-21T10:0' + minute + ':00Z');
  const candidate = {
    id:'current-live', name:'Польша · Варшава', country_code:'pl', endpoint,
    current:true, tested:true, eligible:true, available:true, reachable:true,
    download_mbps:42.5, throughput_source:'strict_aggregate',
    application_rtt_ms:239, vpn_rtt_ms:157, jitter_ms:272,
    media_samples:4, media_stalls:0, service_ok:4, service_total:4
  };
  const display = (speed, measuredAt, historical = false) => ({
    download_mbps:speed, measured_at:measuredAt, historical, throughput_source:'strict_aggregate'
  });
  function result({identity = identities[0], address = endpoint, time = times[0], speed = 42.5, saved = undefined} = {}) {
    const value = {
      success:true, available:true, current_identity:identity, scanned_at:time,
      candidates:[{...candidate, endpoint:address, download_mbps:speed,
        throughput_source:speed > 0 ? 'strict_aggregate' : '',
        eligible:speed > 0, media_samples:speed > 0 ? 4 : 2,
        rejections:speed > 0 ? [] : ['Недостаточно завершённых замеров скорости']}]
    };
    if (saved !== null && (saved || speed > 0)) value.display_throughput = saved || display(speed, time);
    return value;
  }
  const until = async (condition, message) => {
    const deadline = Date.now() + 10000;
    while (!condition() && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 20));
    assert.ok(condition(), message);
  };
  const memorySource = fs.readFileSync(path.join(root, 'freenet-ui/overview_current_quality_memory.go'), 'utf8');
  const memoryMatch = memorySource.match(/<script id="freenetOverviewCurrentQualityMemory">([\s\S]*?)<\/script>/);
  assert.ok(memoryMatch, 'execute the actual embedded first-paint helper');

  async function fixture() {
    const page = await browser.newPage({viewport:{width:1440,height:1000}, timezoneId:'UTC'});
    const calls = [], errors = [], gates = [], jobs = new Map();
    const state = {
      status:{version:'0.8.0-beta.17', country:'Польша', city:'Варшава', country_code:'pl',
        endpoint, current_identity:identities[0], xray_online:true, xkeen_ui_online:true,
        dns_out_present:true, dns_mode:'xkeen', setup_complete:true,
        install_scenario:'existing_stack', subscription_configured:true,
        busy:false, updater_busy:false, mutation_blocked:false},
      manualResult:result(), cacheResult:null, bestResult:{success:true,candidates:[]},
      manualGate:null, bestGate:null, automaticGate:null, automaticMode:'job', automaticResult:result({time:times[2],speed:66.6})
    };
    const gate = () => {
      let release;
      const promise = new Promise(resolve => { release = resolve; });
      const value = {promise, release};
      gates.push(value);
      return value;
    };
    const answer = (route, body, code = 200, headers = {}) => route.fulfill({
      status:code, contentType:'application/json', headers, body:JSON.stringify(body)
    });
    page.on('pageerror', error => errors.push(error.message));
    await page.route('**/api/**', async route => {
      const request = route.request(), url = new URL(request.url());
      calls.push({path:url.pathname, query:url.search, method:request.method(), identity:state.status.current_identity});
      if (request.method() !== 'GET') return answer(route, {success:false,error:'mutation forbidden in quality fixture'}, 403);
      if (url.pathname === '/api/auth/status') return answer(route, {configured:true,authenticated:true});
      if (url.pathname === '/api/status') return answer(route, state.status);
      if (url.pathname === '/api/xray/service') return answer(route, {success:true,version:'v26.9.9',current_version:'v26.9.9'});
      if (url.pathname === '/api/network-profile/plan') return answer(route, {success:true,supported:true,active:true,extra_profiles:[
        {id:'fixture-quality-target',name:'DE Германия',country_code:'de',address:'192.0.2.20',port:443}
      ]});
      if (url.pathname === '/api/vpn/best-foreign') {
        const body = state.bestResult, waiting = state.bestGate;
        if (waiting) await waiting.promise;
        return answer(route, body);
      }
      if (url.pathname === '/api/vpn/current-quality') {
        const mode = url.searchParams.get('job'), id = url.searchParams.get('id');
        if (mode === 'cache') return answer(route, state.cacheResult || {success:false}, state.cacheResult ? 200 : 404);
        if (mode === 'status') {
          assert.equal(url.searchParams.has('automatic'), false, 'automatic is an admission flag, never a polling flag');
          const job = jobs.get(id);
          assert.ok(job, 'poll only the admitted quality job');
          if (job.gate) await job.gate.promise;
          return answer(route, {id,mode:'current',state:'completed',result:job.result}, 202);
        }
        assert.equal(mode, 'start', 'quality must use the existing job start path');
        assert.match(id || '', /^[a-zA-Z0-9_-]{16,64}$/);
        if (url.searchParams.get('automatic') === '1') {
          if (state.automaticMode === 'cooldown') return answer(route, {success:false,error:'automatic retry cooldown'}, 409, {'Retry-After':'600'});
          if (state.automaticMode === 'cache') return answer(route, state.automaticResult);
          jobs.set(id, {result:state.automaticResult,gate:state.automaticGate});
          return answer(route, {id,mode:'current',state:'running',stage:'quality',completed:0,total:1}, 202);
        }
        assert.equal(url.searchParams.has('automatic'), false, 'the explicit button is independent of automatic cooldown');
        const body = state.manualResult, waiting = state.manualGate;
        await new Promise(resolve => setTimeout(resolve, 120));
        if (waiting) await waiting.promise;
        return answer(route, body);
      }
      return answer(route, {success:true,available:false,configured:true,active:false});
    });
    await page.goto(base);
    await page.waitForFunction(() => document.querySelector('#bestCurrentEndpoint')?.textContent === '192.0.2.10:443');
    await page.waitForFunction(() => document.querySelector('#bestServerCheckCurrent') && !document.querySelector('#bestServerCheckCurrent').disabled);
    const snapshot = () => page.evaluate(() => {
      const caption = document.querySelector('#bestCurrentQuality');
      return {
        speed:document.querySelector('#bestCurrentMetrics [data-metric="speed"] b')?.textContent || '',
        http:document.querySelector('#bestCurrentMetrics [data-metric="http"] b')?.textContent || '',
        checkedAt:caption?.dataset.checkedAt || '', measuredAt:caption?.dataset.speedMeasuredAt || '',
        caption:caption?.textContent || ''
      };
    });
    const waitSpeed = speed => page.waitForFunction(value => document.querySelector('#bestCurrentMetrics [data-metric="speed"] b')?.textContent === value, speed);
    const manual = async () => {
      await page.locator('#bestServerCheckCurrent').click();
      await page.waitForFunction(() => !document.querySelector('#bestServerCheckCurrent').disabled);
    };
    const publish = body => page.evaluate(data => {
      document.dispatchEvent(new CustomEvent('freenet:current-quality-display', {detail:{
        candidate:data.candidates[0], scanned_at:data.scanned_at,
        current_identity:data.current_identity, display_throughput:data.display_throughput
      }}));
    }, body);
    const status = async patch => {
      state.status = {...state.status, ...patch};
      await page.evaluate(() => loadStatus());
    };
    const reenter = async () => {
      const cacheRead = page.waitForResponse(response => {
        const url = new URL(response.url());
        return url.pathname === '/api/vpn/current-quality' && url.searchParams.get('job') === 'cache';
      });
      const left = await page.evaluate(() => {
        setPage('settings');
        return document.querySelector('.page[data-page-view="overview"]').classList.contains('active');
      });
      assert.equal(left, false, 'navigate to a supported page before reentering Overview');
      const entered = await page.evaluate(() => {
        setPage('overview');
        return document.querySelector('.page[data-page-view="overview"]').classList.contains('active');
      });
      assert.equal(entered, true);
      await (await cacheRead).finished();
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(resolve)));
    };
    const automatic = () => calls.filter(call => call.path === '/api/vpn/current-quality' && new URLSearchParams(call.query).get('automatic') === '1');
    const close = async () => {
      gates.forEach(value => value.release());
      assert.equal(calls.filter(call => call.method !== 'GET').length, 0, 'quality display and retry never mutate VPN configuration');
      assert.deepEqual(errors, [], 'production scripts run without browser errors');
      await page.close();
    };
    return {page,state,calls,gate,snapshot,waitSpeed,manual,publish,status,reenter,automatic,close};
  }

  const view = await fixture();
  try {
    await view.manual();
    assert.equal((await view.snapshot()).speed, '42.5 Мбит/с');
    assert.equal((await view.snapshot()).checkedAt, times[0]);
    assert.equal((await view.snapshot()).measuredAt, times[0]);

    view.state.manualResult = result({time:times[1],speed:0,saved:display(42.5,times[0],true)});
    const waiting = view.gate();
    view.state.manualGate = waiting;
    await view.page.locator('#bestServerCheckCurrent').click();
    const refreshing = await view.snapshot();
    assert.equal(refreshing.speed, '42.5 Мбит/с', 'refresh retains the measured number');
    assert.match(refreshing.caption, /Обновляем…/);
    assert.equal(refreshing.measuredAt, times[0], 'refresh does not retime the retained speed');
    waiting.release();
    await view.page.waitForFunction(() => !document.querySelector('#bestServerCheckCurrent').disabled);
    view.state.manualGate = null;
    const partial = await view.snapshot();
    assert.equal(partial.speed, '42.5 Мбит/с', 'last successful display speed survives an incomplete current check');
    assert.equal(partial.checkedAt, times[1], 'connection check time advances');
    assert.equal(partial.measuredAt, times[0], 'historical throughput keeps the original measurement time');
    assert.match(partial.caption, /замер.*21\.10.*10:01/);
    assert.doesNotMatch(partial.caption, /критери|Speedtest|поток|ошиб|причин|из 4/i);
    assert.equal(await view.page.locator('#bestCurrentHealth').textContent(), 'VPN работает.',
      'available VPN with 239 ms HTTP is not presented as a failed connection');

    const alternative = {...candidate,id:'alternative',name:'Германия',endpoint:'192.0.2.20:443',current:false,download_mbps:80,application_rtt_ms:150,jitter_ms:4};
    view.state.bestResult = {success:true,scanned_at:times[5],candidates:[view.state.manualResult.candidates[0],alternative],
      recommendation:alternative,profiles_scanned:2,selection_token:'f'.repeat(32)};
    await view.page.locator('#bestServerRefresh').click();
    await view.page.waitForFunction(() => !document.querySelector('#bestServerRefresh').disabled);
    assert.deepEqual(await view.snapshot(), partial, 'comparison scan timestamp cannot retime the cached current baseline');
    assert.doesNotMatch(await view.page.locator('#bestServerReason').textContent(), /Скорость [＋+−-]/,
      'historical display throughput never becomes a decision/comparison candidate speed');

    view.state.manualResult = {success:true,candidates:[],scanned_at:times[2]};
    await view.manual();
    assert.deepEqual(await view.snapshot(), partial, 'empty response keeps both actual timestamps');
    const staleResponses = [
      result({time:times[0],speed:90}),
      result({time:times[1],speed:91}),
      result({time:'invalid-time',speed:92}),
      result({time:'2027-02-30T10:00:00Z',speed:93}),
      result({time:times[3],speed:94,address:'192.0.2.99:443'}),
      result({time:times[3],speed:95,identity:identities[1]})
    ];
    for (const old of staleResponses) {
      await view.publish(old);
      assert.deepEqual(await view.snapshot(), partial, 'old/equal/invalid or differently bound cache is ignored');
    }
    await view.publish(result({time:times[2],speed:55.5}));
    await view.waitSpeed('55.5 Мбит/с');

    const oldRequest = view.gate();
    view.state.manualGate = oldRequest;
    view.state.manualResult = result({time:times[3],speed:99});
    await view.page.locator('#bestServerCheckCurrent').click();
    await until(() => view.calls.filter(call => call.path === '/api/vpn/current-quality' && new URLSearchParams(call.query).get('job') === 'start').length === 4, 'old current request has started');
    await view.status({current_identity:identities[1]});
    assert.equal((await view.snapshot()).speed, '—', 'same endpoint with new authoritative identity clears prior metrics');
    oldRequest.release();
    await view.page.waitForFunction(() => !document.querySelector('#bestServerCheckCurrent').disabled);
    view.state.manualGate = null;
    assert.equal((await view.snapshot()).speed, '—', 'late explicit result from the prior identity is rejected too');
    assert.equal((await view.snapshot()).checkedAt, '');
    await view.publish(result({time:times[4],speed:99}));
    assert.equal((await view.snapshot()).speed, '—', 'late cache cannot restore a previous logical profile');
    await view.publish(result({identity:identities[1],time:times[4],speed:77.7}));
    await view.waitSpeed('77.7 Мбит/с');

    const oldBest = view.gate();
    view.state.bestGate = oldBest;
    view.state.bestResult = {...view.state.bestResult,candidates:[result({identity:identities[1],speed:77.7}).candidates[0],alternative]};
    await view.page.locator('#bestServerRefresh').click();
    await until(() => view.calls.filter(call => call.path === '/api/vpn/best-foreign').length === 2, 'Best request captured its current baseline');
    await view.status({current_identity:identities[0]});
    oldBest.release();
    await view.page.waitForFunction(() => !document.querySelector('#bestServerRefresh').disabled);
    view.state.bestGate = null;
    assert.equal((await view.snapshot()).http, '—', 'late Best baseline cannot import prior-identity RTT/health under the new status identity');
    assert.equal((await view.snapshot()).checkedAt, '');
    assert.equal(await view.page.locator('.vpn-option-apply').count(), 1, 'current baseline rejection leaves alternative selection policy unchanged');

    const nextEndpoint = '192.0.2.30:443';
    await view.status({endpoint:nextEndpoint,current_identity:identities[2]});
    assert.equal((await view.snapshot()).speed, '—', 'endpoint change also clears retained throughput');
    await view.publish(result({identity:identities[1],time:times[5],speed:99}));
    assert.equal((await view.snapshot()).speed, '—');
    view.state.manualResult = result({identity:identities[2],address:nextEndpoint,time:times[5],speed:0,saved:null});
    await view.manual();
    const unknown = await view.snapshot();
    assert.equal(unknown.speed, '—', 'missing throughput is unknown, never a measured zero');
    assert.equal(unknown.measuredAt, '');
    assert.equal(unknown.checkedAt, times[5]);
    assert.doesNotMatch(unknown.caption, /0(?:\.0)? Мбит|ошиб|критери|из 4/i);
    const failed = result({identity:identities[2],address:nextEndpoint,time:times[6],speed:0,saved:display(31.4,times[4],true)});
    failed.available = false;
    failed.candidates[0] = {...failed.candidates[0],available:false,reachable:false,application_rtt_ms:0,vpn_rtt_ms:0};
    view.state.manualResult = failed;
    await view.manual();
    assert.equal((await view.snapshot()).speed, '31.4 Мбит/с', 'failed latest connectivity can retain only the separate historical measurement');
    assert.equal((await view.snapshot()).measuredAt, times[4]);
    assert.equal(await view.page.locator('#bestCurrentHealth').textContent(), 'Интернет через VPN недоступен.', 'retained speed must not hide actual route failure');
    await view.status({country:'',country_code:'',city:'',profile_label:'Профиль без кода страны'});
    assert.equal((await view.snapshot()).speed, '31.4 Мбит/с', 'status events without a flag do not change quality identity');
  } finally {
    await view.close();
  }

  const auto = await fixture();
  try {
    auto.state.cacheResult = result({time:times[1],speed:0,saved:display(42.5,times[0],true)});
    const finish = auto.gate();
    auto.state.automaticGate = finish;
    await auto.page.locator('#fnVpnPickerV2Toggle').click();
    await auto.page.locator('#fnVpnPickerV2Results [data-profile-id="fixture-quality-target"]').click();
    await auto.page.waitForFunction(() => !document.querySelector('#fnVpnPickerV2Connect').disabled);
    assert.equal(await auto.page.locator('#exactConnectBtn').isDisabled(), false, 'picker fixture has a genuinely ready manual choice before automatic work');
    await auto.page.addScriptTag({content:memoryMatch[1]});
    await until(() => auto.automatic().length === 1, 'incomplete cache starts one automatic measurement despite saved display speed');
    await auto.waitSpeed('42.5 Мбит/с');
    assert.match((await auto.snapshot()).caption, /Обновляем…/);
    assert.equal(await auto.page.locator('#bestServerCheckCurrent').isDisabled(), true, 'automatic measurement blocks a duplicate manual check immediately');
    assert.equal(await auto.page.locator('#bestServerRefresh').isDisabled(), true);
    assert.equal(await auto.page.locator('#bestServerAdvanced').evaluate(node => node.inert), true, 'manual apply stays fenced during the quiet measurement');
    assert.equal(await auto.page.locator('.vpn-current-panel>.action-row').evaluate(node => node.inert), true);
    await auto.page.waitForFunction(() => document.querySelector('#fnVpnPickerV2Connect').disabled);
    assert.equal(await auto.page.locator('#exactConnectBtn').isDisabled(), false, 'automatic UI fence does not rewrite the underlying readiness state');
    await auto.page.evaluate(() => document.querySelector('#fnVpnPickerV2Connect').click());
    assert.equal(auto.calls.filter(call => call.method !== 'GET').length, 0, 'top-level picker cannot apply during the quiet measurement');
    await auto.page.evaluate(() => { buttonsBusy(false); document.querySelector('#bestServerCheckCurrent').click(); });
    assert.equal(await auto.page.locator('#bestServerCheckCurrent').isDisabled(), true, 'an idle status poll cannot override the automatic operation');
    finish.release();
    await auto.waitSpeed('66.6 Мбит/с');
    await auto.page.waitForFunction(() => !document.querySelector('#bestCurrentQuality').textContent.includes('Обновляем'));
    assert.equal(await auto.page.locator('#bestServerCheckCurrent').isDisabled(), false, 'automatic completion releases the UI fence');
    assert.equal(await auto.page.locator('#bestServerAdvanced').evaluate(node => node.inert), false);
    await auto.page.waitForFunction(() => !document.querySelector('#fnVpnPickerV2Connect').disabled);
    await auto.page.locator('#fnVpnPickerV2Close').click();
    assert.equal((await auto.snapshot()).measuredAt, times[2], '202 completed job publishes the fresh measurement');
    await auto.reenter();
    assert.equal(auto.automatic().length, 1, 'reentering Overview cannot repeat an automatic attempt');
    assert.equal((await auto.snapshot()).speed, '66.6 Мбит/с', 'late partial cache cannot replace completed automatic result');

    auto.state.manualResult = result({time:times[3],speed:0,saved:display(66.6,times[2],true)});
    await auto.manual();
    await auto.page.waitForTimeout(200);
    assert.equal(auto.automatic().length, 1, 'a partial manual result cannot restart an already used page/identity retry');
    assert.equal((await auto.snapshot()).measuredAt, times[2]);

    auto.state.automaticMode = 'cache';
    auto.state.automaticGate = null;
    auto.state.cacheResult = result({identity:identities[1],time:times[1],speed:0,saved:display(77.7,times[0],true)});
    auto.state.automaticResult = result({identity:identities[1],time:times[4],speed:88.8});
    await auto.status({current_identity:identities[1]});
    assert.equal((await auto.snapshot()).speed, '—');
    await auto.reenter();
    await auto.waitSpeed('88.8 Мбит/с');
    assert.equal(auto.automatic().length, 2, 'new logical identity has one independent attempt');
    assert.equal((await auto.snapshot()).checkedAt, times[4], '200 cached success from automatic start is published without a job');

    auto.state.automaticMode = 'cooldown';
    auto.state.cacheResult = result({identity:identities[2],time:times[3],speed:0,saved:display(25,times[1],true)});
    await auto.status({current_identity:identities[2]});
    await auto.reenter();
    await until(() => auto.automatic().length === 3, 'server cooldown receives only one admission request');
    await auto.waitSpeed('25.0 Мбит/с');
    await auto.page.waitForFunction(() => !document.querySelector('#bestCurrentQuality').textContent.includes('Обновляем'));
    assert.doesNotMatch((await auto.snapshot()).caption, /cooldown|ошиб|повтор|600/i, 'cooldown ends quietly with the saved value');
    await auto.reenter();
    assert.equal(auto.automatic().length, 3, '409 never loops or retries on Overview navigation');

    auto.state.cacheResult = result({identity:identities[3],time:times[3],speed:0,saved:null});
    await auto.status({current_identity:identities[3],busy:true});
    await auto.reenter();
    assert.equal(auto.automatic().length, 3, 'active operation prevents automatic retry');
    await auto.status({busy:false,updater_busy:true});
    await auto.reenter();
    assert.equal(auto.automatic().length, 3, 'updater ownership prevents automatic retry');
    await auto.status({updater_busy:false,mutation_blocked:true});
    await auto.reenter();
    assert.equal(auto.automatic().length, 3, 'protected pending/unknown state prevents automatic retry');

    await auto.status({mutation_blocked:false});
    const busyUI = auto.gate();
    auto.state.manualGate = busyUI;
    auto.state.manualResult = result({identity:identities[3],time:times[4],speed:30});
    await auto.page.locator('#bestServerCheckCurrent').click();
    await auto.reenter();
    assert.equal(auto.automatic().length, 3, 'an active explicit check prevents seed admission even before server busy polling');
    busyUI.release();
    await auto.page.waitForFunction(() => !document.querySelector('#bestServerCheckCurrent').disabled);
    auto.state.manualGate = null;
    auto.state.automaticMode = 'cache';
    auto.state.automaticResult = result({identity:identities[3],time:times[6],speed:31.4});
    auto.state.manualResult = result({identity:identities[3],time:times[5],speed:0,saved:display(30,times[4],true)});
    await auto.manual();
    await auto.waitSpeed('31.4 Мбит/с');
    assert.equal(auto.automatic().length, 4, 'fresh partial manual check can use the one still-unused quiet retry');
    assert.equal((await auto.snapshot()).measuredAt, times[6]);
  } finally {
    await auto.close();
  }
  console.log('PASS: current speed display timestamps, source/identity ordering, late manual rejection and one bounded quiet automatic retry.');
};
