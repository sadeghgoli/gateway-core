const apiBase = location.pathname.includes("/_admin") ? "/_admin/api" : "/api";
const statusFa = { up: "وصل", down: "قطع", degraded: "ناپایدار", unknown: "نامشخص", off: "غیرفعال" };

async function api(path, opts = {}) {
  const res = await fetch(apiBase + path, {
    credentials: "include",
    headers: { "Content-Type": "application/json", ...(opts.headers || {}) },
    ...opts,
  });
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  if (!res.ok) throw new Error((data && data.error) || res.statusText);
  return data;
}

const loginView = document.getElementById("login-view");
const app = document.getElementById("app");
const NODE_W = 260;
const NODE_H = 88;
let graph = { nodes: [], edges: [] };
let view = { x: 64, y: 48, scale: 1 };
let canvasInited = false;
let drag = null;
let pointers = new Map();
let didFitOnce = false;

document.getElementById("login-btn").onclick = async () => {
  document.getElementById("login-err").textContent = "";
  try {
    await api("/login", { method: "POST", body: JSON.stringify({ username: user.value, password: pass.value }) });
    boot();
  } catch (e) { document.getElementById("login-err").textContent = e.message; }
};

document.getElementById("logout").onclick = async () => { await api("/logout", { method: "POST", body: "{}" }); boot(); };
document.getElementById("btn-new").onclick = () => editGateway(blankGateway());
document.getElementById("btn-nginx").onclick = openNginx;
document.getElementById("btn-health").onclick = async () => { await api("/health?probe=1"); await renderCanvas(); };

document.querySelectorAll(".rail-btn").forEach((b) => {
  b.onclick = () => {
    document.querySelectorAll(".rail-btn").forEach((x) => x.classList.remove("active"));
    b.classList.add("active");
    document.querySelectorAll(".tab").forEach((t) => t.classList.add("hidden"));
    document.getElementById("tab-" + b.dataset.tab).classList.remove("hidden");
    if (b.dataset.tab === "list") loadList();
    if (b.dataset.tab === "queue") loadQueue();
  };
});

async function boot() {
  try {
    await api("/me");
    loginView.classList.add("hidden");
    app.classList.remove("hidden");
    initCanvas();
    await renderCanvas();
  } catch {
    loginView.classList.remove("hidden");
    app.classList.add("hidden");
  }
}

async function renderCanvas() {
  graph = await api("/graph");
  const box = document.getElementById("nodes");
  box.innerHTML = "";
  graph.nodes.forEach((n) => {
    const el = document.createElement("div");
    el.className = `node ${n.type} ${n.status || ""}`;
    el.dataset.id = n.id;
    el.style.left = n.x + "px";
    el.style.top = n.y + "px";
    el.innerHTML = `
      <div class="port in"></div><div class="port out"></div>
      <div class="muted">${n.type === "domain" ? "دامنه عمومی" : "مقصد"}</div>
      <h3>${esc(n.label)}</h3>
      <div class="muted">${esc(n.subtitle || "")}</div>
      <span class="badge ${n.status}">${statusFa[n.status] || n.status}</span>`;
    el.addEventListener("pointerdown", (ev) => onNodePointerDown(ev, n, el));
    el.addEventListener("click", () => {
      if (el.dataset.dragged === "1") return;
      if (n.gateway_id) openGateway(n.gateway_id);
    });
    box.appendChild(el);
  });
  drawWires();
  applyView();
  if (!didFitOnce && graph.nodes.length) {
    didFitOnce = true;
    fitView();
  }
}

function initCanvas() {
  if (canvasInited) return;
  canvasInited = true;
  const wrap = document.getElementById("canvas-wrap");
  wrap.addEventListener("pointerdown", onCanvasPointerDown);
  window.addEventListener("pointermove", onCanvasPointerMove);
  window.addEventListener("pointerup", onCanvasPointerUp);
  window.addEventListener("pointercancel", onCanvasPointerUp);
  wrap.addEventListener("wheel", onCanvasWheel, { passive: false });
  wrap.addEventListener("dblclick", (ev) => {
    if (ev.target.closest(".node") || ev.target.closest("#canvas-hud")) return;
    zoomAt(ev.clientX, ev.clientY, view.scale * 1.25);
  });
  wrap.addEventListener("contextmenu", (ev) => ev.preventDefault());
  document.getElementById("zoom-in").onclick = () => zoomAroundCenter(1.2);
  document.getElementById("zoom-out").onclick = () => zoomAroundCenter(1 / 1.2);
  document.getElementById("zoom-fit").onclick = () => fitView();
  window.addEventListener("keydown", (ev) => {
    if (app.classList.contains("hidden")) return;
    if (ev.target && ["INPUT", "TEXTAREA", "SELECT"].includes(ev.target.tagName)) return;
    if (ev.code === "Digit0" && (ev.ctrlKey || ev.metaKey)) {
      ev.preventDefault();
      fitView();
    }
  });
}

function applyView() {
  const world = document.getElementById("canvas-world");
  if (!world) return;
  world.style.transform = `translate(${view.x}px, ${view.y}px) scale(${view.scale})`;
  const pct = document.getElementById("zoom-pct");
  if (pct) pct.textContent = Math.round(view.scale * 100) + "%";
}

function clampScale(s) {
  return Math.min(3, Math.max(0.2, s));
}

function wrapRect() {
  return document.getElementById("canvas-wrap").getBoundingClientRect();
}

function zoomAt(clientX, clientY, nextScale) {
  const rect = wrapRect();
  const s = clampScale(nextScale);
  const wx = (clientX - rect.left - view.x) / view.scale;
  const wy = (clientY - rect.top - view.y) / view.scale;
  view.scale = s;
  view.x = clientX - rect.left - wx * s;
  view.y = clientY - rect.top - wy * s;
  applyView();
}

function zoomAroundCenter(factor) {
  const rect = wrapRect();
  zoomAt(rect.left + rect.width / 2, rect.top + rect.height / 2, view.scale * factor);
}

function fitView() {
  const wrap = document.getElementById("canvas-wrap");
  if (!wrap || !graph.nodes.length) {
    view = { x: 64, y: 48, scale: 1 };
    applyView();
    return;
  }
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  graph.nodes.forEach((n) => {
    minX = Math.min(minX, n.x);
    minY = Math.min(minY, n.y);
    maxX = Math.max(maxX, n.x + NODE_W);
    maxY = Math.max(maxY, n.y + NODE_H);
  });
  const pad = 72;
  const bw = Math.max(maxX - minX, 1) + pad * 2;
  const bh = Math.max(maxY - minY, 1) + pad * 2;
  const s = clampScale(Math.min(wrap.clientWidth / bw, wrap.clientHeight / bh, 1.15));
  view.scale = s;
  view.x = (wrap.clientWidth - (minX + maxX) * s) / 2;
  view.y = (wrap.clientHeight - (minY + maxY) * s) / 2;
  applyView();
}

function onCanvasWheel(ev) {
  if (ev.target.closest("#canvas-hud")) return;
  ev.preventDefault();
  const pinchZoom = ev.ctrlKey || ev.metaKey;
  const trackpadPan = !pinchZoom && (Math.abs(ev.deltaX) > 0.5 || ev.shiftKey);
  if (trackpadPan) {
    view.x -= ev.shiftKey ? ev.deltaY : ev.deltaX;
    view.y -= ev.shiftKey ? 0 : ev.deltaY;
    applyView();
    return;
  }
  zoomAt(ev.clientX, ev.clientY, view.scale * Math.exp(-ev.deltaY * 0.0018));
}

function onNodePointerDown(ev, n, el) {
  if (ev.button !== 0) return;
  ev.stopPropagation();
  ev.preventDefault();
  el.setPointerCapture(ev.pointerId);
  el.dataset.dragged = "0";
  drag = {
    kind: "node",
    id: ev.pointerId,
    n,
    el,
    lastX: ev.clientX,
    lastY: ev.clientY,
    moved: false,
  };
}

function onCanvasPointerDown(ev) {
  if (ev.target.closest("#canvas-hud")) return;
  if (ev.target.closest(".node")) return;
  if (ev.button !== 0 && ev.button !== 1) return;
  ev.preventDefault();
  const wrap = document.getElementById("canvas-wrap");
  wrap.setPointerCapture(ev.pointerId);
  pointers.set(ev.pointerId, { x: ev.clientX, y: ev.clientY });
  if (pointers.size === 2) {
    const pts = [...pointers.values()];
    const dist = Math.hypot(pts[1].x - pts[0].x, pts[1].y - pts[0].y);
    const mx = (pts[0].x + pts[1].x) / 2;
    const my = (pts[0].y + pts[1].y) / 2;
    const rect = wrapRect();
    drag = {
      kind: "pinch",
      dist,
      scale: view.scale,
      wx: (mx - rect.left - view.x) / view.scale,
      wy: (my - rect.top - view.y) / view.scale,
    };
    return;
  }
  wrap.classList.add("panning");
  drag = { kind: "pan", lastX: ev.clientX, lastY: ev.clientY, moved: false };
}

function onCanvasPointerMove(ev) {
  if (pointers.has(ev.pointerId)) pointers.set(ev.pointerId, { x: ev.clientX, y: ev.clientY });
  if (!drag) return;
  if (drag.kind === "pinch" && pointers.size >= 2) {
    const pts = [...pointers.values()];
    const dist = Math.hypot(pts[1].x - pts[0].x, pts[1].y - pts[0].y);
    if (dist < 8 || drag.dist < 8) return;
    const mx = (pts[0].x + pts[1].x) / 2;
    const my = (pts[0].y + pts[1].y) / 2;
    const s = clampScale(drag.scale * (dist / drag.dist));
    const rect = wrapRect();
    view.scale = s;
    view.x = mx - rect.left - drag.wx * s;
    view.y = my - rect.top - drag.wy * s;
    applyView();
    return;
  }
  if (drag.kind === "pan") {
    const dx = ev.clientX - drag.lastX;
    const dy = ev.clientY - drag.lastY;
    if (Math.abs(dx) + Math.abs(dy) > 2) drag.moved = true;
    view.x += dx;
    view.y += dy;
    drag.lastX = ev.clientX;
    drag.lastY = ev.clientY;
    applyView();
    return;
  }
  if (drag.kind === "node" && drag.id === ev.pointerId) {
    const dx = (ev.clientX - drag.lastX) / view.scale;
    const dy = (ev.clientY - drag.lastY) / view.scale;
    if (Math.abs(dx) + Math.abs(dy) > 0.5) {
      drag.moved = true;
      drag.el.dataset.dragged = "1";
      drag.el.classList.add("dragging");
    }
    drag.n.x = Math.max(8, drag.n.x + dx);
    drag.n.y = Math.max(8, drag.n.y + dy);
    drag.el.style.left = drag.n.x + "px";
    drag.el.style.top = drag.n.y + "px";
    drag.lastX = ev.clientX;
    drag.lastY = ev.clientY;
    drawWires();
  }
}

async function onCanvasPointerUp(ev) {
  pointers.delete(ev.pointerId);
  const wrap = document.getElementById("canvas-wrap");
  wrap.classList.remove("panning");
  if (drag && drag.kind === "pinch") {
    if (pointers.size < 2) drag = pointers.size === 1 ? { kind: "pan", lastX: [...pointers.values()][0].x, lastY: [...pointers.values()][0].y, moved: true } : null;
    return;
  }
  if (drag && drag.kind === "node" && drag.id === ev.pointerId) {
    drag.el.classList.remove("dragging");
    if (drag.moved) {
      try {
        await api("/layout", { method: "POST", body: JSON.stringify(graph.nodes.map((x) => ({ id: x.id, x: x.x, y: x.y }))) });
      } catch { /* ignore */ }
    }
    drag = null;
    return;
  }
  if (drag && drag.kind === "pan") drag = null;
}

function drawWires() {
  const svg = document.getElementById("wires");
  const byId = Object.fromEntries(graph.nodes.map((n) => [n.id, n]));
  svg.innerHTML = graph.edges.map((e) => {
    const a = byId[e.from], b = byId[e.to];
    if (!a || !b) return "";
    const x1 = a.x + 260, y1 = a.y + 42;
    const x2 = b.x, y2 = b.y + 42;
    const c = Math.max(60, Math.abs(x2 - x1) / 2);
    const color = e.status === "down" ? "#ef5b5b" : e.status === "up" ? "#30c48d" : e.status === "degraded" ? "#e6b84d" : "#8b8ba3";
    return `<path d="M ${x1} ${y1} C ${x1 + c} ${y1}, ${x2 - c} ${y2}, ${x2} ${y2}" stroke="${color}" fill="none" stroke-width="2.4"/>`;
  }).join("");
}

function blankGateway() {
  return {
    name: "", host: "", enabled: true, lb_strategy: "single",
    max_concurrency: 100, queue_size: 200, queue_timeout_ms: 5000, rps: 0,
    sensitive: false, cors_allow_origin: "", websocket: true, client_max_body: "20m",
    proxy_read_timeout: 120, proxy_send_timeout: 120, nginx_extra: "", health_path: "/",
    allowed_origins: [], access_tokens: [],
    upstreams: [{ kind: "local", target_host: "127.0.0.1", target_port: 8081, scheme: "http", url: "", weight: 1, enabled: true, health_path: "/" }],
    routes: [{ path_prefix: "/", path_regex: "", strip_prefix: "", add_prefix: "", set_query: {}, remove_query: [], set_headers: { "X-Forwarded-Gateway": "" }, priority: 0 }],
  };
}

async function openGateway(id) {
  const g = await api("/gateways?id=" + encodeURIComponent(id));
  editGateway(g);
}

function editGateway(g) {
  showDrawer(`
    <h2 style="margin-top:0;">اتصال دامنه</h2>
    <label>نام</label><input id="e-name" value="${esc(g.name || "")}" />
    <label>دامنه عمومی</label><input id="e-host" value="${esc(g.host || "")}" placeholder="map-gateway.sabzevar.ir" />
    <label>پورت عمومی Nginx<input id="e-lp" type="number" value="${g.listen_port || 443}" /></label>
    <p class="muted">در حالت ۴۴۳ مشترک همه دامنه‌ها روی ۴۴۳ هستند. فقط اگر «پورت جدا» در تنظیمات Nginx فعال باشد، از ۸۰۰۰ به بالا پورت آزاد گرفته می‌شود.</p>
    <label><input id="e-en" type="checkbox" ${g.enabled !== false ? "checked" : ""}/> فعال</label>
    <label><input id="e-sens" type="checkbox" ${g.sensitive ? "checked" : ""}/> حساس (لاگین)</label>
    <label><input id="e-ws" type="checkbox" ${g.websocket !== false ? "checked" : ""}/> WebSocket در Nginx</label>
    <div class="row">
      <label style="flex:1">body size Nginx<input id="e-body" value="${esc(g.client_max_body || "")}" placeholder="20m" /></label>
      <label style="flex:1">read timeout<input id="e-rt" type="number" value="${g.proxy_read_timeout || 0}" /></label>
      <label style="flex:1">send timeout<input id="e-st" type="number" value="${g.proxy_send_timeout || 0}" /></label>
    </div>
    <div class="row">
      <label style="flex:1">همزمانی<input id="e-conc" type="number" value="${g.max_concurrency || 100}" /></label>
      <label style="flex:1">صف<input id="e-q" type="number" value="${g.queue_size || 200}" /></label>
      <label style="flex:1">RPS<input id="e-rps" type="number" value="${g.rps || 0}" /></label>
    </div>
    <label>استراتژی LB
      <select id="e-lb"><option value="single">single</option><option value="round_robin">round_robin</option></select>
    </label>
    <label>مسیر سلامت</label><input id="e-hp" value="${esc(g.health_path || "/")}" />
    <label>CORS</label><input id="e-cors" value="${esc(g.cors_allow_origin || "")}" />
    <h3>چه کسی اجازه استفاده دارد</h3>
    <p class="muted">اگر خالی بماند همه می‌توانند. اگر دامنه یا توکن بگذارید، درخواست باید از همان سایت بیاید یا توکن معتبر داشته باشد (یکی کافی است).</p>
    <label>دامنه‌های مجاز وب (هر خط یکی)</label>
    <textarea id="e-origins" rows="3" placeholder="map.sabzevar.ir&#10;*.myapp.ir">${esc((g.allowed_origins || []).join("\n"))}</textarea>
    <div id="toks"></div>
    <button class="secondary" id="add-tok" type="button">+ توکن اپلیکیشن</button>
    <h3>مقصدها</h3>
    <p class="muted">local یعنی دامنه به پورت همین سرور وصل می‌شود. remote یعنی دامنه/URL دیگر.</p>
    <div id="ups"></div>
    <button class="secondary" id="add-up">+ مقصد</button>
    <label>دستور اضافه Nginx (هر خط یک directive)</label>
    <textarea id="e-nx" rows="3">${esc(g.nginx_extra || "")}</textarea>
    <label>مسیرها JSON</label>
    <textarea id="e-rtj" rows="6">${esc(JSON.stringify(g.routes || [], null, 2))}</textarea>
    <div class="row">
      <button id="e-save">ذخیره</button>
      ${g.id ? '<button class="danger" id="e-del">حذف</button>' : ""}
      <button class="secondary" id="e-close">بستن</button>
    </div>
    <div id="e-err" class="err"></div>
  `);
  document.getElementById("e-lb").value = g.lb_strategy || "single";
  const ups = (g.upstreams && g.upstreams.length) ? g.upstreams : blankGateway().upstreams;
  const upsBox = document.getElementById("ups");
  const renderUps = () => {
    upsBox.innerHTML = ups.map((u, i) => `
      <div class="card" style="margin:8px 0;">
        <label>نوع
          <select data-kind="${i}"><option value="local">پورت محلی</option><option value="remote">دامنه / URL</option></select>
        </label>
        <div class="row">
          <label style="flex:1">Host<input data-th="${i}" value="${esc(u.target_host || "127.0.0.1")}" /></label>
          <label style="flex:1">پورت<input data-tp="${i}" type="number" value="${u.target_port || 80}" /></label>
          <label style="flex:1">scheme
            <select data-sc="${i}"><option>http</option><option>https</option></select>
          </label>
        </div>
        <label>URL (برای remote)<input data-url="${i}" value="${esc(u.url || "")}" /></label>
        <label>وزن<input data-w="${i}" type="number" value="${u.weight || 1}" /></label>
        <label><input data-en="${i}" type="checkbox" ${u.enabled !== false ? "checked" : ""}/> فعال</label>
      </div>`).join("");
    ups.forEach((u, i) => {
      upsBox.querySelector(`[data-kind="${i}"]`).value = u.kind || "remote";
      upsBox.querySelector(`[data-sc="${i}"]`).value = u.scheme || "http";
    });
  };
  renderUps();
  const toks = (g.access_tokens && g.access_tokens.length) ? g.access_tokens.map((t) => ({ ...t })) : [];
  const toksBox = document.getElementById("toks");
  const renderToks = () => {
    toksBox.innerHTML = toks.map((t, i) => `
      <div class="card tok-row">
        <label>نام اپ / سرویس<input data-tn="${i}" value="${esc(t.name || "")}" placeholder="اپ اندروید نقشه" /></label>
        <label>توکن API<input data-tt="${i}" class="tok" dir="ltr" value="${esc(t.token || "")}" /></label>
        <div class="row">
          <label><input data-te="${i}" type="checkbox" ${t.enabled !== false ? "checked" : ""}/> فعال</label>
          <button class="secondary" type="button" data-tcopy="${i}">کپی</button>
          <button class="secondary" type="button" data-tgen="${i}">تولید مجدد</button>
          <button class="danger" type="button" data-tdel="${i}">حذف</button>
        </div>
      </div>`).join("") || `<p class="muted">هنوز توکنی نیست. برای اپ موبایل یا سرویس بک‌اند یک توکن بسازید.</p>`;
    toksBox.querySelectorAll("[data-tcopy]").forEach((b) => {
      b.onclick = () => navigator.clipboard.writeText(toks[+b.dataset.tcopy].token || "");
    });
    toksBox.querySelectorAll("[data-tgen]").forEach((b) => {
      b.onclick = () => { toks[+b.dataset.tgen].token = newApiToken(); renderToks(); };
    });
    toksBox.querySelectorAll("[data-tdel]").forEach((b) => {
      b.onclick = () => { toks.splice(+b.dataset.tdel, 1); renderToks(); };
    });
  };
  renderToks();
  document.getElementById("add-tok").onclick = () => {
    toks.push({ id: "", name: "", token: newApiToken(), enabled: true });
    renderToks();
  };
  document.getElementById("add-up").onclick = () => {
    ups.push({ kind: "local", target_host: "127.0.0.1", target_port: 8082, scheme: "http", url: "", weight: 1, enabled: true });
    renderUps();
  };
  document.getElementById("e-close").onclick = hideDrawer;
  const del = document.getElementById("e-del");
  if (del) del.onclick = async () => {
    if (!confirm("حذف شود؟")) return;
    await api("/gateways?id=" + encodeURIComponent(g.id), { method: "DELETE" });
    hideDrawer(); boot();
  };
  document.getElementById("e-save").onclick = async () => {
    try {
      const body = {
        id: g.id || "",
        name: document.getElementById("e-name").value,
        host: document.getElementById("e-host").value,
        listen_port: num("e-lp"),
        enabled: document.getElementById("e-en").checked,
        sensitive: document.getElementById("e-sens").checked,
        websocket: document.getElementById("e-ws").checked,
        client_max_body: document.getElementById("e-body").value,
        proxy_read_timeout: num("e-rt"),
        proxy_send_timeout: num("e-st"),
        max_concurrency: num("e-conc"),
        queue_size: num("e-q"),
        queue_timeout_ms: g.queue_timeout_ms || 5000,
        rps: num("e-rps"),
        lb_strategy: document.getElementById("e-lb").value,
        health_path: document.getElementById("e-hp").value,
        cors_allow_origin: document.getElementById("e-cors").value,
        allowed_origins: document.getElementById("e-origins").value.split(/\r?\n/).map((s) => s.trim()).filter(Boolean),
        access_tokens: toks.map((t, i) => ({
          id: t.id || "",
          name: toksBox.querySelector(`[data-tn="${i}"]`)?.value || t.name,
          token: toksBox.querySelector(`[data-tt="${i}"]`)?.value || t.token,
          enabled: toksBox.querySelector(`[data-te="${i}"]`)?.checked !== false,
        })),
        nginx_extra: document.getElementById("e-nx").value,
        routes: JSON.parse(document.getElementById("e-rtj").value),
        upstreams: ups.map((_, i) => ({
          kind: upsBox.querySelector(`[data-kind="${i}"]`).value,
          target_host: upsBox.querySelector(`[data-th="${i}"]`).value,
          target_port: parseInt(upsBox.querySelector(`[data-tp="${i}"]`).value, 10),
          scheme: upsBox.querySelector(`[data-sc="${i}"]`).value,
          url: upsBox.querySelector(`[data-url="${i}"]`).value,
          weight: parseInt(upsBox.querySelector(`[data-w="${i}"]`).value, 10),
          enabled: upsBox.querySelector(`[data-en="${i}"]`).checked,
        })),
      };
      const res = await api("/gateways", { method: "POST", body: JSON.stringify(body) });
      if (res.nginx_error) document.getElementById("e-err").textContent = "ذخیره شد (پورت " + (res.listen_port || "?") + ")؛ Nginx: " + res.nginx_error;
      else { hideDrawer(); await renderCanvas(); }
    } catch (e) { document.getElementById("e-err").textContent = e.message; }
  };
}

async function openNginx() {
  const ns = await api("/nginx");
  showDrawer(`
    <h2 style="margin-top:0;">کنترل Nginx</h2>
    <p class="muted">گیت‌وی فایل conf را می‌سازد و در صورت فعال بودن، reload می‌کند. ترافیک همچنان از Nginx به این سرویس می‌آید.</p>
    <label><input id="n-man" type="checkbox" ${ns.managed ? "checked" : ""}/> مدیریت‌شده</label>
    <label><input id="n-auto" type="checkbox" ${ns.auto_reload ? "checked" : ""}/> اعمال خودکار بعد از ذخیره گیت‌وی</label>
    <label><input id="n-shared" type="checkbox" ${ns.shared_443 !== false ? "checked" : ""}/> ۴۴۳ مشترک (همه دامنه‌ها روی یک پورت — مدل کارفرما)</label>
    <label>مسیر فایل conf</label><input id="n-path" value="${esc(ns.conf_path || "")}" />
    <label>دستور تست (none = رد شدن)</label><input id="n-test" value="${esc(ns.test_cmd || "")}" />
    <label>دستور reload</label><input id="n-rel" value="${esc(ns.reload_cmd || "")}" />
    <p class="muted">با ۴۴۳ مشترک فقط http/https روی فایروال باز می‌شود و روتینگ با Host در Go انجام می‌شود. پورت جدا فقط برای حالت قدیمی است.</p>
    <div class="row">
      <label style="flex:1">پورت HTTP (ریدایرکت)<input id="n-http" type="number" value="${ns.listen_http || 80}" /></label>
      <label style="flex:1">پورت HTTPS مشترک<input id="n-https" type="number" value="${ns.listen_https || 443}" /></label>
      <label style="flex:1">شروع پورت دامنه‌ها (حالت جدا)<input id="n-dstart" type="number" value="${ns.domain_port_start || 8000}" /></label>
      <label style="flex:1">پایان محدوده<input id="n-dmax" type="number" value="${ns.domain_port_max || 8999}" /></label>
    </div>
    <label>آدرس Go برای Nginx</label><input id="n-up" value="${esc(ns.gateway_upstream || "127.0.0.1:8002")}" />
    <label>گواهی</label><input id="n-cert" value="${esc(ns.ssl_cert || "")}" />
    <label>کلید</label><input id="n-key" value="${esc(ns.ssl_key || "")}" />
    <label><input id="n-redir" type="checkbox" ${ns.redirect_http ? "checked" : ""}/> ریدایرکت HTTP به HTTPS</label>
    <label><input id="n-ws" type="checkbox" ${ns.websocket ? "checked" : ""}/> WebSocket پیش‌فرض</label>
    <label>client_max_body_size</label><input id="n-body" value="${esc(ns.client_max_body || "20m")}" />
    <div class="row">
      <label style="flex:1">read timeout<input id="n-rt" type="number" value="${ns.proxy_read_timeout || 120}" /></label>
      <label style="flex:1">send timeout<input id="n-st" type="number" value="${ns.proxy_send_timeout || 120}" /></label>
    </div>
    <div class="row">
      <button id="n-save">ذخیره تنظیمات</button>
      <button class="secondary" id="n-prev">پیش‌نمایش</button>
      <button id="n-apply">اعمال روی سرور</button>
      <button class="secondary" id="e-close">بستن</button>
    </div>
    <div id="e-err" class="err"></div>
    <pre class="cfg" id="n-cfg"></pre>
  `);
  document.getElementById("e-close").onclick = hideDrawer;
  const collect = () => {
    const shared = document.getElementById("n-shared").checked;
    const https = num("n-https") || 443;
    return {
      managed: document.getElementById("n-man").checked,
      auto_reload: document.getElementById("n-auto").checked,
      shared_443: shared,
      conf_path: document.getElementById("n-path").value,
      test_cmd: document.getElementById("n-test").value,
      reload_cmd: document.getElementById("n-rel").value,
      listen_http: num("n-http"),
      listen_https: https,
      listen_admin_https: shared ? https : (ns.listen_admin_https || 8003),
      domain_port_start: num("n-dstart"),
      domain_port_max: num("n-dmax"),
      gateway_upstream: document.getElementById("n-up").value,
      ssl_cert: document.getElementById("n-cert").value,
      ssl_key: document.getElementById("n-key").value,
      redirect_http: document.getElementById("n-redir").checked,
      websocket: document.getElementById("n-ws").checked,
      client_max_body: document.getElementById("n-body").value,
      proxy_read_timeout: num("n-rt"),
      proxy_send_timeout: num("n-st"),
    };
  };
  document.getElementById("n-save").onclick = async () => {
    try { await api("/nginx", { method: "POST", body: JSON.stringify(collect()) }); document.getElementById("e-err").textContent = "ذخیره شد"; }
    catch (e) { document.getElementById("e-err").textContent = e.message; }
  };
  document.getElementById("n-prev").onclick = async () => {
    try {
      await api("/nginx", { method: "POST", body: JSON.stringify(collect()) });
      const p = await api("/nginx/preview");
      document.getElementById("n-cfg").textContent = p.config || "";
    } catch (e) { document.getElementById("e-err").textContent = e.message; }
  };
  document.getElementById("n-apply").onclick = async () => {
    try {
      await api("/nginx", { method: "POST", body: JSON.stringify(collect()) });
      await api("/nginx/apply", { method: "POST", body: "{}" });
      document.getElementById("e-err").textContent = "اعمال شد";
    } catch (e) { document.getElementById("e-err").textContent = e.message; }
  };
}

async function loadList() {
  const list = await api("/gateways");
  const health = await api("/health");
  const byGw = {};
  (health || []).forEach((h) => { byGw[h.gateway_id] = byGw[h.gateway_id] || []; byGw[h.gateway_id].push(h); });
  document.getElementById("cards").innerHTML = (list || []).map((g) => {
    const hs = (byGw[g.id] || []).map((h) => `<div class="muted">${esc(h.target)} — <span class="badge ${h.status}">${statusFa[h.status] || h.status}</span></div>`).join("");
    return `<div class="card"><div class="muted">${esc(g.host)}${g.listen_port && g.listen_port !== 443 ? ":" + g.listen_port : ""}</div><h3>${esc(g.name)}</h3>${hs || ""}<div class="row"><button class="secondary" data-id="${g.id}">ویرایش</button></div></div>`;
  }).join("");
  document.querySelectorAll("#cards [data-id]").forEach((b) => { b.onclick = () => openGateway(b.dataset.id); });
}

async function loadQueue() {
  const qs = await api("/queues");
  document.getElementById("qbody").innerHTML = (qs || []).map((q) =>
    `<tr><td>${esc(q.name)}<br><span class="muted">${esc(q.host)}</span></td><td>${q.inflight}</td><td>${q.waiting}</td><td>${q.rejected}</td><td>${q.max_concurrency}</td><td>${q.rps || "∞"}</td></tr>`
  ).join("");
}

function showDrawer(html) {
  const d = document.getElementById("drawer");
  d.classList.remove("hidden");
  document.querySelector(".workspace").classList.remove("nodrawer");
  d.innerHTML = html;
}
function hideDrawer() {
  const d = document.getElementById("drawer");
  d.classList.add("hidden");
  d.innerHTML = "";
}
function num(id) { return parseInt(document.getElementById(id).value, 10) || 0; }
function esc(s) { return String(s).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;"); }
function newApiToken() {
  const a = new Uint8Array(24);
  crypto.getRandomValues(a);
  return Array.from(a, (b) => b.toString(16).padStart(2, "0")).join("");
}

setInterval(() => { if (!app.classList.contains("hidden") && !drag) renderCanvas().catch(() => {}); }, 15000);
boot();
