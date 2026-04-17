(() => {
  "use strict";

  const csrfToken = () => {
    const el = document.querySelector('meta[name="csrf-token"]');
    return el ? el.content : "";
  };

  async function api(method, url, body, { form = false } = {}) {
    const headers = { "X-CSRF-Token": csrfToken() };
    const init = { method, headers, credentials: "same-origin" };
    if (body !== undefined && body !== null) {
      if (form) {
        init.body = body;
      } else {
        headers["Content-Type"] = "application/json";
        init.body = JSON.stringify(body);
      }
    }
    const resp = await fetch(url, init);
    const ct = resp.headers.get("Content-Type") || "";
    let data = null;
    if (ct.includes("application/json")) {
      data = await resp.json().catch(() => null);
    } else {
      data = await resp.text().catch(() => "");
    }
    return { ok: resp.ok, status: resp.status, data };
  }

  function showBanner(host, cls, text) {
    host.querySelectorAll(".banner").forEach((b) => b.remove());
    const div = document.createElement("div");
    div.className = "banner " + cls;
    div.textContent = text;
    host.prepend(div);
  }

  function errorText(data, fallback) {
    if (data && data.error && data.error.message) return data.error.message;
    return fallback;
  }

  function initRating(widget) {
    const filename = widget.getAttribute("data-filename");
    const buttons = widget.querySelectorAll("button[data-rating]");
    buttons.forEach((btn) => {
      btn.addEventListener("click", async () => {
        const val = parseInt(btn.getAttribute("data-rating"), 10);
        if (!Number.isFinite(val)) return;
        const next = btn.getAttribute("aria-pressed") === "true" ? null : val;
        const resp = await api(
          "PATCH",
          "/api/books/" + encodeURIComponent(filename),
          { rating: next },
        );
        if (resp.ok) {
          buttons.forEach((b) => {
            const v = parseInt(b.getAttribute("data-rating"), 10);
            b.setAttribute("aria-pressed", next !== null && v <= next ? "true" : "false");
          });
        } else if (resp.status === 409) {
          showBanner(widget.closest("main") || document.body, "warn",
            errorText(resp.data, "This note changed outside Shelf. Reload and try again."));
        } else {
          showBanner(widget.closest("main") || document.body, "error",
            errorText(resp.data, "Could not save rating."));
        }
      });
    });
  }

  function initStatus(el) {
    const filename = el.getAttribute("data-filename");
    const select = el.querySelector("select");
    if (!select) return;
    select.addEventListener("change", async () => {
      const resp = await api(
        "PATCH",
        "/api/books/" + encodeURIComponent(filename),
        { status: select.value },
      );
      if (!resp.ok) {
        showBanner(el.closest("main") || document.body,
          resp.status === 409 ? "warn" : "error",
          errorText(resp.data, "Could not save status."));
      } else {
        showBanner(el.closest("main") || document.body, "ok", "Status saved. Reload to see updated timeline.");
      }
    });
  }

  function initReview(form) {
    const filename = form.getAttribute("data-filename");
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      const ta = form.querySelector("textarea[name=review]");
      const resp = await api(
        "PATCH",
        "/api/books/" + encodeURIComponent(filename),
        { review: ta ? ta.value : "" },
      );
      if (!resp.ok) {
        showBanner(form.closest("main") || document.body,
          resp.status === 409 ? "warn" : "error",
          errorText(resp.data, "Could not save review."));
      } else {
        showBanner(form.closest("main") || document.body, "ok", "Review saved.");
      }
    });
  }

  function initImport() {
    const planForm = document.getElementById("plan-form");
    const applyBtn = document.getElementById("apply-btn");
    const planOut = document.getElementById("plan-output");
    const reportOut = document.getElementById("apply-report");
    if (!planForm || !applyBtn || !planOut) return;

    let currentCSV = null;
    let currentPlan = null;

    planForm.addEventListener("submit", async (e) => {
      e.preventDefault();
      const fd = new FormData(planForm);
      const csv = fd.get("csv");
      if (!csv || (csv instanceof File && csv.size === 0)) {
        showBanner(planOut, "error", "Select a CSV file first.");
        return;
      }
      currentCSV = csv;
      const resp = await api("POST", "/api/import/plan", fd, { form: true });
      if (!resp.ok) {
        showBanner(planOut, "error", errorText(resp.data, "Plan request failed."));
        applyBtn.disabled = true;
        return;
      }
      currentPlan = resp.data;
      renderPlan(planOut, currentPlan);
      applyBtn.disabled = false;
    });

    applyBtn.addEventListener("click", async () => {
      if (!currentCSV || !currentPlan) return;
      const fd = new FormData();
      fd.set("csv", currentCSV);
      fd.set("decisions", JSON.stringify(collectDecisions(planOut)));
      applyBtn.disabled = true;
      const resp = await api("POST", "/api/import/apply", fd, { form: true });
      if (!resp.ok) {
        showBanner(reportOut || planOut, "error", errorText(resp.data, "Apply request failed."));
        applyBtn.disabled = false;
        return;
      }
      renderReport(reportOut || planOut, resp.data);
    });
  }

  function renderPlan(host, plan) {
    host.textContent = "";
    const header = document.createElement("h2");
    header.textContent = "Plan preview";
    host.appendChild(header);

    const summary = document.createElement("p");
    summary.textContent =
      (plan.will_create || []).length + " create, " +
      (plan.will_update || []).length + " update, " +
      (plan.will_skip || []).length + " skip, " +
      (plan.conflicts || []).length + " conflict";
    host.appendChild(summary);

    host.appendChild(makeSection("Will create", plan.will_create || [], "diff-create",
      (e) => [e.filename, e.reason]));
    host.appendChild(makeSection("Will update", plan.will_update || [], "diff-update",
      (e) => {
        const changes = (e.changes || [])
          .map((c) => c.field + ": " + JSON.stringify(c.old) + " → " + JSON.stringify(c.new))
          .join("; ");
        return [e.filename, e.reason + " (" + changes + ")"];
      }));
    host.appendChild(makeSection("Will skip", plan.will_skip || [], "diff-skip",
      (e) => [e.filename, e.reason]));

    const conflictsSection = document.createElement("section");
    const h = document.createElement("h3");
    h.textContent = "Conflicts (" + (plan.conflicts || []).length + ")";
    conflictsSection.appendChild(h);
    (plan.conflicts || []).forEach((c, idx) => {
      const row = document.createElement("div");
      row.className = "diff-conflict";
      row.style.padding = "0.5rem";
      row.style.marginBottom = "0.5rem";
      row.setAttribute("data-conflict-row", String(idx));
      row.setAttribute("data-filename", c.filename);

      const fn = document.createElement("strong");
      fn.textContent = c.filename;
      row.appendChild(fn);
      const reason = document.createElement("p");
      reason.textContent = c.reason;
      row.appendChild(reason);

      const accept = makeRadio("conflict_" + idx, "accept", "Accept", false);
      const skip = makeRadio("conflict_" + idx, "skip", "Skip", true);
      row.appendChild(accept);
      row.appendChild(skip);
      conflictsSection.appendChild(row);
    });
    host.appendChild(conflictsSection);
  }

  function makeSection(title, entries, cls, renderEntry) {
    const section = document.createElement("section");
    const h = document.createElement("h3");
    h.textContent = title + " (" + entries.length + ")";
    section.appendChild(h);
    if (entries.length === 0) {
      const p = document.createElement("p");
      p.textContent = "None.";
      p.style.color = "var(--muted)";
      section.appendChild(p);
      return section;
    }
    const table = document.createElement("table");
    table.className = "diff-table " + cls;
    entries.forEach((e) => {
      const [name, detail] = renderEntry(e);
      const tr = document.createElement("tr");
      const td1 = document.createElement("td");
      td1.textContent = name;
      const td2 = document.createElement("td");
      td2.textContent = detail;
      tr.appendChild(td1);
      tr.appendChild(td2);
      table.appendChild(tr);
    });
    section.appendChild(table);
    return section;
  }

  function makeRadio(name, value, label, checked) {
    const l = document.createElement("label");
    l.style.marginRight = "0.75rem";
    const i = document.createElement("input");
    i.type = "radio";
    i.name = name;
    i.value = value;
    if (checked) i.checked = true;
    l.appendChild(i);
    l.appendChild(document.createTextNode(" " + label));
    return l;
  }

  function collectDecisions(host) {
    const out = [];
    host.querySelectorAll("[data-conflict-row]").forEach((row) => {
      const filename = row.getAttribute("data-filename");
      const checked = row.querySelector('input[type=radio]:checked');
      out.push({ filename, action: checked ? checked.value : "skip" });
    });
    return out;
  }

  function renderReport(host, report) {
    host.textContent = "";
    const h = document.createElement("h2");
    h.textContent = "Apply report";
    host.appendChild(h);

    const p = document.createElement("p");
    p.textContent =
      (report.created || []).length + " created, " +
      (report.updated || []).length + " updated, " +
      (report.skipped || []).length + " skipped, " +
      (report.errors || []).length + " errors.";
    host.appendChild(p);

    const backup = document.createElement("p");
    backup.textContent = "Backup: " + (report.backup_root || "(none)");
    host.appendChild(backup);

    if ((report.errors || []).length > 0) {
      const ul = document.createElement("ul");
      report.errors.forEach((e) => {
        const li = document.createElement("li");
        li.textContent = e.filename + " (" + e.phase + "): " + e.error;
        ul.appendChild(li);
      });
      host.appendChild(ul);
    }
  }

  document.addEventListener("DOMContentLoaded", () => {
    document.querySelectorAll("[data-rating-widget]").forEach(initRating);
    document.querySelectorAll("[data-status-select]").forEach(initStatus);
    document.querySelectorAll("[data-review-form]").forEach(initReview);
    initImport();
  });
})();
