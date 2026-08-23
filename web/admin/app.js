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
let graph = { nodes: [], edges: [] };
let dragging = null;

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
    el.onmousedown = (ev) => startDrag(ev, n, el);
    el.onclick = (ev) => {
      if (el.dataset.dragged === "1") return;
      if (n.gateway_id) openGateway(n.gateway_id);
    };
    box.appendChild(el);
  });
  drawWires();
}

function startDrag(ev, n, el) {
  if (ev.button !== 0) return;
  el.dataset.dragged = "0";
  dragging = { n, el, x: ev.clientX, y: ev.clientY, ox: n.x, oy: n.y };
  window.onmousemove = (e) => {
    if (!dragging) return;
    const dx = e.clientX - dragging.x;
    const dy = e.clientY - dragging.y;
    if (Math.abs(dx) + Math.abs(dy) > 3) el.dataset.dragged = "1";
    n.x = Math.max(20, dragging.ox + dx);
    n.y = Math.max(20, dragging.oy + dy);
    el.style.left = n.x + "px";
    el.style.top = n.y + "px";
    drawWires();
  };
  window.onmouseup = async () => {
    window.onmousemove = null;
    window.onmouseup = null;
    if (!dragging) return;
    await api("/layout", { method: "POST", body: JSON.stringify(graph.nodes.map((x) => ({ id: x.id, x: x.x, y: x.y }))) });
    dragging = null;
  };
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
      if (res.nginx_error) document.getElementById("e-err").textContent = "ذخیره شد؛ Nginx: " + res.nginx_error;
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
    <label>مسیر فایل conf</label><input id="n-path" value="${esc(ns.conf_path || "")}" />
    <label>دستور تست (none = رد شدن)</label><input id="n-test" value="${esc(ns.test_cmd || "")}" />
    <label>دستور reload</label><input id="n-rel" value="${esc(ns.reload_cmd || "")}" />
    <div class="row">
      <label style="flex:1">پورت HTTP<input id="n-http" type="number" value="${ns.listen_http || 80}" /></label>
      <label style="flex:1">پورت HTTPS گیت‌وی‌ها<input id="n-https" type="number" value="${ns.listen_https || 443}" /></label>
      <label style="flex:1">پورت پنل ادمین (HTTP)<input id="n-admin" type="number" value="${ns.listen_admin_https || 8003}" /></label>
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
  const collect = () => ({
    managed: document.getElementById("n-man").checked,
    auto_reload: document.getElementById("n-auto").checked,
    conf_path: document.getElementById("n-path").value,
    test_cmd: document.getElementById("n-test").value,
    reload_cmd: document.getElementById("n-rel").value,
    listen_http: num("n-http"),
    listen_https: num("n-https"),
    listen_admin_https: num("n-admin"),
    gateway_upstream: document.getElementById("n-up").value,
    ssl_cert: document.getElementById("n-cert").value,
    ssl_key: document.getElementById("n-key").value,
    redirect_http: document.getElementById("n-redir").checked,
    websocket: document.getElementById("n-ws").checked,
    client_max_body: document.getElementById("n-body").value,
    proxy_read_timeout: num("n-rt"),
    proxy_send_timeout: num("n-st"),
  });
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
    return `<div class="card"><div class="muted">${esc(g.host)}</div><h3>${esc(g.name)}</h3>${hs || ""}<div class="row"><button class="secondary" data-id="${g.id}">ویرایش</button></div></div>`;
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

setInterval(() => { if (!app.classList.contains("hidden")) renderCanvas().catch(() => {}); }, 15000);
boot();
