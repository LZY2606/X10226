// Minimal vanilla-JS review console. All identities shown in the page come
// from the server; this script only performs actions and reloads the durable
// server-rendered view, so refresh or another browser shows the same state.
const TGA = (() => {
  async function api(method, url, body) {
    const res = await fetch(url, {
      method,
      headers: body ? {"Content-Type": "application/json"} : undefined,
      body: body ? JSON.stringify(body) : undefined,
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.message || res.statusText);
    return data;
  }

  function flash(msg, ok = true) {
    const el = document.getElementById("flash");
    if (!el) return;
    el.textContent = msg;
    el.className = "pill " + (ok ? "ok" : "err");
  }

  function goto(path) { window.location = path; }

  async function createProject(form) {
    const name = form.querySelector("[name=name]").value;
    const caseId = form.querySelector("[name=caseId]").value;
    const p = await api("POST", "/api/projects", {name, caseId, seed: 20260919});
    goto("/projects/" + encodeURIComponent(p.id));
  }

  async function instantiateCase(btn) {
    const cid = btn.dataset.cid;
    btn.disabled = true;
    try {
      const r = await api("POST", `/api/cases/${encodeURIComponent(cid)}/instantiate`, {});
      flash(`实例化项目；导入接受 ${r.initialImport.accepted} 层，拒绝 ${r.initialImport.rejected} 层`);
      setTimeout(() => goto("/projects/" + encodeURIComponent(r.project.id)), 600);
    } catch (e) { flash(String(e), false); btn.disabled = false; }
  }

  async function submitBundle(form) {
    const text = form.querySelector("[name=text]").value;
    try {
      const r = await api("POST", `${projectURL()}/import-bundle`, {text});
      flash(`导入完成：接受 ${r.accepted}，拒绝(半失败) ${r.rejected}`);
      setTimeout(() => window.location.reload(), 500);
    } catch (e) { flash(String(e), false); }
  }

  async function submitLayer(form) {
    const layer = form.querySelector("[name=layer]").value;
    const text = form.querySelector("[name=text]").value;
    try {
      const r = await api("POST", `${projectURL()}/import-layer`, {layer, text});
      const ok = r.payload.accepted;
      flash(ok ? "层修订已追加为新版本" : "半失败：片段与诊断已保留，当前层未改变", ok);
      setTimeout(() => window.location.reload(), 500);
    } catch (e) { flash(String(e), false); }
  }

  async function reviseParams(form) {
    const params = {};
    form.querySelectorAll("[data-param]").forEach(i => { params[i.dataset.param] = parseFloat(i.value); });
    try {
      await api("POST", `${projectURL()}/params`, {params, reason: form.querySelector("[name=reason]").value});
      flash("参数新版本已提交，相关派生结果已重算");
      setTimeout(() => window.location.reload(), 400);
    } catch (e) { flash(String(e), false); }
  }

  async function rule(btn) {
    const candidateKey = btn.dataset.key;
    const decision = btn.dataset.decision;
    const rationale = document.getElementById(`rationale-${candidateKey}`).value || "";
    try {
      await api("POST", `${projectURL()}/rulings`, {candidateKey, decision, rationale});
      flash(`已记录裁定：${decision}`);
      setTimeout(() => window.location.reload(), 300);
    } catch (e) { flash(String(e), false); }
  }

  async function undo(btn) {
    const rulingId = btn.dataset.ruling;
    try {
      await api("POST", `${projectURL()}/undo`, {rulingId, reason: "manual undo from review page"});
      flash("撤销已写入历史（原裁定保留但失活）");
      setTimeout(() => window.location.reload(), 300);
    } catch (e) { flash(String(e), false); }
  }

  async function resolveConflict(form) {
    const data = Object.fromEntries(new FormData(form).entries());
    try {
      await api("POST", `${projectURL()}/resolve`, data);
      flash("冲突解决已记录");
      setTimeout(() => window.location.reload(), 300);
    } catch (e) { flash(String(e), false); }
  }

  function timeTravel(seq) {
    window.location = `${projectURL()}?at=${seq}`;
  }

  async function exportArchive() {
    const res = await fetch(`${projectURL()}/export`);
    const blob = await res.blob();
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = "tga-archive.json";
    a.click();
    flash("已导出事件档案（派生节点由事件重放重建）");
  }

  async function importArchive(input) {
    const file = input.files[0];
    if (!file) return;
    const text = await file.text();
    try {
      const r = await api("POST", "/api/import-archive", text);
      flash(`导入成功：项目 ${r.projectId}，事件 ${r.events} 条，身份已校验`);
      setTimeout(() => goto("/projects/" + encodeURIComponent(r.projectId)), 700);
    } catch (e) { flash(String(e), false); }
  }

  function projectURL() {
    return "/api/projects/" + encodeURIComponent(document.body.dataset.project);
  }

  // synchronized crosshair across the four panels (time-domain alignment)
  function syncCrosshair() {
    const svgs = [...document.querySelectorAll("svg.chart")];
    if (!svgs.length) return;
    const wrap = document.createElement("div");
    document.body.appendChild(wrap);
    svgs.forEach(svgEl => {
      const line = document.createElementNS("http://www.w3.org/2000/svg", "line");
      line.setAttribute("y1", "0"); line.setAttribute("y2", "100%");
      line.setAttribute("class", "crosshair");
      line.style.stroke = "#ffffff66"; line.style.strokeWidth = "1";
      line.setAttribute("visible", "false");
      svgEl.appendChild(line);
      svgEl.addEventListener("mousemove", ev => {
        const rect = svgEl.getBoundingClientRect();
        const vb = svgEl.viewBox.baseVal;
        const x = (ev.clientX - rect.left) / rect.width * vb.width;
        svgs.forEach(other => {
          const l = other.querySelector(".crosshair");
          l.setAttribute("x1", x); l.setAttribute("x2", x);
          l.setAttribute("y1", 22); l.setAttribute("y2", vb.height - 30);
          l.setAttribute("visible", "true");
        });
      });
      svgEl.addEventListener("mouseleave", () => {
        svgs.forEach(o => o.querySelector(".crosshair").setAttribute("visible", "false"));
      });
    });
  }

  document.addEventListener("DOMContentLoaded", () => {
    document.body.addEventListener("click", ev => {
      const t = ev.target;
      if (t.dataset.act === "instantiate") instantiateCase(t);
      if (t.dataset.act === "rule") rule(t);
      if (t.dataset.act === "undo") undo(t);
      if (t.dataset.act === "export") exportArchive();
    });
    document.body.addEventListener("change", ev => {
      if (ev.target.dataset.act === "import-archive") importArchive(ev.target);
    });
    syncCrosshair();
  });

  return {api, createProject, submitBundle, submitLayer, reviseParams, resolveConflict, timeTravel, goto};
})();
