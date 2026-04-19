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

  // withBusy disables a button and marks data-busy="true" for the
  // duration of an async op so the CSS spinner glyph renders. Guards
  // against double-submit when the user click-spams.
  function withBusy(btn, fn) {
    if (!btn) return Promise.resolve(fn());
    if (btn.dataset.busy === "true") return Promise.resolve();
    btn.dataset.busy = "true";
    btn.setAttribute("aria-busy", "true");
    const prev = btn.disabled;
    btn.disabled = true;
    return Promise.resolve()
      .then(fn)
      .finally(() => {
        delete btn.dataset.busy;
        btn.removeAttribute("aria-busy");
        btn.disabled = prev;
      });
  }

  function toastRegion() {
    let r = document.getElementById("toast-region");
    if (r) return r;
    r = document.createElement("div");
    r.id = "toast-region";
    r.setAttribute("aria-live", "polite");
    r.setAttribute("role", "status");
    document.body.appendChild(r);
    return r;
  }

  // toast shows an ephemeral bottom-right notification. cls ∈
  // {ok, warn, error}. Auto-dismisses after `ms`.
  function toast(cls, text, opts) {
    const ms = (opts && typeof opts.ms === "number") ? opts.ms : 3200;
    const el = document.createElement("div");
    el.className = "toast toast--" + cls;
    el.textContent = text;
    toastRegion().appendChild(el);
    // Next frame: flip the --in class so the transition plays.
    requestAnimationFrame(() => el.classList.add("toast--in"));
    setTimeout(() => {
      el.classList.remove("toast--in");
      setTimeout(() => el.remove(), 240);
    }, ms);
  }

  // initRatingGrid wires the five-axis Trial-System rating widget on
  // the book-detail page. Behavior:
  //   - Star click: paint 1..N as pressed on the clicked axis (or
  //     clear if the user clicks the already-pressed max star).
  //   - "+" bump: append a star button at position N+1 and select it,
  //     letting axis values grow above 5.
  //   - Override checkbox: enables/disables the override input and
  //     includes or omits `overall` in the PATCH payload.
  //   - Override input: lets the user set an explicit 0..10 overall.
  //   - Overall display updates live via the aria-live="polite" output.
  //   - Save is debounced (200 ms trailing) so rapid edits coalesce
  //     into a single PATCH with the full rating state.
  //   - On 409 (stale note), the page soft-reloads after a toast —
  //     the user's click is lost, but they immediately see fresh state.
  function initRatingGrid(grid) {
    const filename = grid.getAttribute("data-filename");
    const overallOutput = grid.querySelector("[data-overall-output]");
    const overrideCheckbox = grid.querySelector("[data-override-checkbox]");
    const overrideInput = grid.querySelector("[data-override-input]");

    function axisEls() { return grid.querySelectorAll(".rating-axis"); }

    function axisMax(axEl) {
      let max = 0;
      axEl.querySelectorAll("button[data-rating]").forEach((b) => {
        if (b.getAttribute("aria-pressed") === "true") {
          const v = parseInt(b.getAttribute("data-rating"), 10);
          if (Number.isFinite(v) && v > max) max = v;
        }
      });
      return max;
    }

    function paintAxis(axEl, val) {
      axEl.querySelectorAll("button[data-rating]").forEach((b) => {
        const v = parseInt(b.getAttribute("data-rating"), 10);
        b.setAttribute("aria-pressed", val > 0 && v <= val ? "true" : "false");
      });
    }

    function readState() {
      const trial = {};
      axisEls().forEach((ax) => {
        const key = ax.getAttribute("data-axis");
        const v = axisMax(ax);
        if (v > 0) trial[key] = v;
      });
      const hasOverride = overrideCheckbox && overrideCheckbox.checked;
      let overall = null;
      if (hasOverride && overrideInput && overrideInput.value !== "") {
        const parsed = parseFloat(overrideInput.value);
        if (Number.isFinite(parsed)) overall = parsed;
      }
      return { trial_system: trial, overall };
    }

    function computeEffective(state) {
      if (state.overall !== null) return state.overall;
      const vals = Object.values(state.trial_system);
      if (vals.length === 0) return null;
      let sum = 0;
      vals.forEach((v) => { sum += v; });
      return sum / vals.length;
    }

    function formatOverall(v) {
      if (v === null) return "—";
      const rounded = Math.round(v * 10) / 10;
      return Number.isInteger(rounded) ? String(rounded) : rounded.toFixed(1);
    }

    function updateOverallDisplay() {
      if (!overallOutput) return;
      const s = readState();
      overallOutput.textContent = "Overall: " + formatOverall(computeEffective(s)) + "/5";
    }

    function makeStarButton(n, axisLabel) {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "rating-star";
      btn.setAttribute("data-rating", String(n));
      btn.setAttribute("aria-pressed", "true");
      btn.setAttribute("aria-label", n + " for " + axisLabel);
      const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
      svg.setAttribute("class", "icon icon-star");
      svg.setAttribute("aria-hidden", "true");
      svg.setAttribute("focusable", "false");
      const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
      use.setAttribute("href", "#icon-star-filled");
      svg.appendChild(use);
      btn.appendChild(svg);
      return btn;
    }

    // Debounced save: one PATCH per trailing 200ms window carries the
    // full rating state. Rollback snapshot is captured before the wait.
    let saveTimer = null;
    let pendingSnapshot = null;
    function snapshotGrid() {
      const snap = [];
      axisEls().forEach((ax) => {
        const btns = Array.from(ax.querySelectorAll("button[data-rating]"))
          .map((b) => ({
            rating: b.getAttribute("data-rating"),
            pressed: b.getAttribute("aria-pressed"),
            label: b.getAttribute("aria-label"),
          }));
        snap.push({ key: ax.getAttribute("data-axis"), buttons: btns });
      });
      return {
        axes: snap,
        overrideChecked: overrideCheckbox ? overrideCheckbox.checked : false,
        overrideValue: overrideInput ? overrideInput.value : "",
        overrideDisabled: overrideInput ? overrideInput.disabled : true,
      };
    }
    function restoreGrid(snap) {
      if (!snap) return;
      snap.axes.forEach((axSnap, i) => {
        const ax = axisEls()[i];
        if (!ax) return;
        const stars = ax.querySelector(".rating-axis-stars");
        if (!stars) return;
        // Re-render to match exactly what the snapshot held.
        stars.textContent = "";
        const legendEl = ax.querySelector("legend");
        const axisLabel = legendEl ? legendEl.textContent : "";
        axSnap.buttons.forEach((bs) => {
          const n = parseInt(bs.rating, 10);
          const btn = makeStarButton(n, axisLabel);
          btn.setAttribute("aria-pressed", bs.pressed);
          if (bs.label) btn.setAttribute("aria-label", bs.label);
          stars.appendChild(btn);
        });
      });
      if (overrideCheckbox) overrideCheckbox.checked = snap.overrideChecked;
      if (overrideInput) {
        overrideInput.value = snap.overrideValue;
        overrideInput.disabled = snap.overrideDisabled;
      }
      updateOverallDisplay();
    }

    function scheduleSave() {
      if (!pendingSnapshot) pendingSnapshot = snapshotGrid();
      if (saveTimer) clearTimeout(saveTimer);
      saveTimer = setTimeout(doSave, 200);
    }

    async function doSave() {
      saveTimer = null;
      const snap = pendingSnapshot;
      pendingSnapshot = null;
      const state = readState();
      const payload = Object.keys(state.trial_system).length === 0 && state.overall === null
        ? { rating: null }
        : { rating: { trial_system: state.trial_system, overall: state.overall } };
      const resp = await api(
        "PATCH",
        "/api/books/" + encodeURIComponent(filename),
        payload,
      );
      if (resp.ok) {
        toast("ok", payload.rating === null ? "Rating cleared" : "Rating saved");
        return;
      }
      if (resp.status === 409) {
        toast("warn", "This note changed outside Shelf — reloading.");
        setTimeout(() => window.location.reload(), 600);
        return;
      }
      restoreGrid(snap);
      toast("error", errorText(resp.data, "Could not save rating."));
    }

    // Event delegation: one listener covers all existing and
    // dynamically-added star buttons, the "+" bump buttons, and the
    // override controls.
    grid.addEventListener("click", (e) => {
      const star = e.target.closest("button[data-rating]");
      if (star) {
        const ax = star.closest(".rating-axis");
        if (!ax) return;
        const val = parseInt(star.getAttribute("data-rating"), 10);
        if (!Number.isFinite(val)) return;
        const currentMax = axisMax(ax);
        const next = star.getAttribute("aria-pressed") === "true" && val === currentMax
          ? 0
          : val;
        paintAxis(ax, next);
        updateOverallDisplay();
        scheduleSave();
        return;
      }
      const bump = e.target.closest("button[data-bump]");
      if (bump) {
        const ax = bump.closest(".rating-axis");
        if (!ax) return;
        const stars = ax.querySelector(".rating-axis-stars");
        if (!stars) return;
        const next = axisMax(ax) + 1;
        const legendEl = ax.querySelector("legend");
        const axisLabel = legendEl ? legendEl.textContent : "";
        // Ensure every position 1..next has a button (user may click
        // "+" on an unrated axis — fill from 1 up to the new max).
        for (let n = stars.querySelectorAll("button[data-rating]").length + 1; n <= next; n++) {
          stars.appendChild(makeStarButton(n, axisLabel));
        }
        paintAxis(ax, next);
        updateOverallDisplay();
        scheduleSave();
      }
    });

    if (overrideCheckbox) {
      overrideCheckbox.addEventListener("change", () => {
        const on = overrideCheckbox.checked;
        if (overrideInput) {
          overrideInput.disabled = !on;
          if (!on) overrideInput.value = "";
        }
        updateOverallDisplay();
        scheduleSave();
      });
    }
    if (overrideInput) {
      overrideInput.addEventListener("input", () => {
        updateOverallDisplay();
        scheduleSave();
      });
    }
  }

  function initStatus(el) {
    const filename = el.getAttribute("data-filename");
    const select = el.querySelector("select");
    if (!select) return;
    select.addEventListener("change", async () => {
      select.disabled = true;
      try {
        const resp = await api(
          "PATCH",
          "/api/books/" + encodeURIComponent(filename),
          { status: select.value },
        );
        if (!resp.ok) {
          toast(resp.status === 409 ? "warn" : "error",
            errorText(resp.data, "Could not save status."));
        } else {
          toast("ok", "Status updated to " + select.value);
        }
      } finally {
        select.disabled = false;
      }
    });
  }

  function initReview(form) {
    const filename = form.getAttribute("data-filename");
    form.addEventListener("submit", (e) => {
      e.preventDefault();
      const submitBtn = form.querySelector("button[type=submit]");
      return withBusy(submitBtn, async () => {
        const ta = form.querySelector("textarea[name=review]");
        const resp = await api(
          "PATCH",
          "/api/books/" + encodeURIComponent(filename),
          { review: ta ? ta.value : "" },
        );
        if (!resp.ok) {
          toast(resp.status === 409 ? "warn" : "error",
            errorText(resp.data, "Could not save review."));
        } else {
          toast("ok", "Review saved");
        }
      });
    });
  }

  function initImport() {
    const planForm = document.getElementById("plan-form");
    const applyBtn = document.getElementById("apply-btn");
    const planOut = document.getElementById("plan-output");
    const reportOut = document.getElementById("apply-report");
    if (!planForm || !applyBtn || !planOut) return;
    // The /sync page reuses the same element IDs for parallel plumbing.
    // Distinguish by the form's class so each init wires only its own
    // page's handlers (no double-fires, no cross-contamination).
    if (!planForm.classList.contains("import-plan-form")) return;

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
      row.className = "diff-conflict-row";
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
      p.className = "muted";
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
    l.className = "conflict-radio";
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

  // initSync wires the /sync page. Mirrors initImport but (a) the plan
  // request has no CSV (the server fetches Audiobookshelf state itself),
  // (b) the plan shape carries will_update/conflicts/will_skip/unmatched
  // (no will_create — sync never creates notes in v0.2), (c) the apply
  // body is application/x-www-form-urlencoded instead of multipart.
  //
  // Parallel to initImport rather than refactored-shared: the diverging
  // plan shape (unmatched vs. will_create) and transport (urlencoded vs.
  // multipart) make a spec-list refactor more complex than two tight
  // functions. makeSection, makeRadio, collectDecisions remain shared.
  function initSync() {
    const planForm = document.getElementById("plan-form");
    const applyBtn = document.getElementById("apply-btn");
    const planOut = document.getElementById("plan-output");
    const reportOut = document.getElementById("apply-report");
    // When the page is in the disabled-state branch, these are absent;
    // early-return cleanly so we don't fight the import-page handler
    // that looks for the same IDs.
    if (!planForm || !applyBtn || !planOut) return;
    if (!planForm.classList.contains("sync-plan-form")) return;

    let currentPlan = null;

    planForm.addEventListener("submit", async (e) => {
      e.preventDefault();
      await withBusy(planForm.querySelector('button[type="submit"]'), async () => {
        const resp = await api("POST", "/api/sync/audiobookshelf/plan", null, { form: true });
        if (!resp.ok) {
          showBanner(planOut, "error", errorText(resp.data, "Plan request failed."));
          applyBtn.disabled = true;
          currentPlan = null;
          return;
        }
        currentPlan = resp.data;
        renderSyncPlan(planOut, currentPlan);
        applyBtn.disabled = false;
        toast("ok", "Plan generated");
      });
    });

    applyBtn.addEventListener("click", async () => {
      if (!currentPlan) return;
      await withBusy(applyBtn, async () => {
        const fd = new FormData();
        fd.set("decisions", JSON.stringify(collectDecisions(planOut)));
        const resp = await api("POST", "/api/sync/audiobookshelf/apply", fd, { form: true });
        if (!resp.ok) {
          showBanner(reportOut || planOut, "error", errorText(resp.data, "Apply request failed."));
          return;
        }
        renderSyncReport(reportOut || planOut, resp.data);
        toast("ok", "Sync applied");
      });
    });
  }

  function renderSyncPlan(host, plan) {
    host.textContent = "";
    const header = document.createElement("h2");
    header.textContent = "Plan preview";
    host.appendChild(header);

    const summary = document.createElement("p");
    summary.textContent =
      (plan.will_update || []).length + " update, " +
      (plan.conflicts || []).length + " conflict, " +
      (plan.will_skip || []).length + " skip, " +
      (plan.unmatched || []).length + " unmatched";
    host.appendChild(summary);

    host.appendChild(makeSection("Will update", plan.will_update || [], "diff-update",
      (e) => [e.filename, e.reason]));
    host.appendChild(makeSection("Will skip", plan.will_skip || [], "diff-skip",
      (e) => [e.filename, e.reason]));
    host.appendChild(makeSection("Unmatched Audiobookshelf items", plan.unmatched || [], "diff-skip",
      (e) => [e.display_title + (e.display_author ? " — " + e.display_author : ""), e.reason]));

    const conflictsSection = document.createElement("section");
    const h = document.createElement("h3");
    h.textContent = "Conflicts (" + (plan.conflicts || []).length + ")";
    conflictsSection.appendChild(h);
    (plan.conflicts || []).forEach((c, idx) => {
      const row = document.createElement("div");
      row.className = "diff-conflict-row";
      row.setAttribute("data-conflict-row", String(idx));
      row.setAttribute("data-filename", c.filename);

      const fn = document.createElement("strong");
      fn.textContent = c.filename;
      row.appendChild(fn);
      const reason = document.createElement("p");
      reason.textContent = c.reason;
      row.appendChild(reason);

      const accept = makeRadio("sync_conflict_" + idx, "accept", "Accept", false);
      const skip = makeRadio("sync_conflict_" + idx, "skip", "Skip", true);
      row.appendChild(accept);
      row.appendChild(skip);
      conflictsSection.appendChild(row);
    });
    host.appendChild(conflictsSection);
  }

  function renderSyncReport(host, report) {
    host.textContent = "";
    const h = document.createElement("h2");
    h.textContent = "Apply report";
    host.appendChild(h);

    // Audiobookshelf sync never creates notes in v0.2 (unmatched items
    // are reported, not auto-created), so the summary omits "created".
    const p = document.createElement("p");
    p.textContent =
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

  // Add-book page: ISBN + search → preview cards → Add.
  function initAddPage() {
    if (!document.querySelector('main[data-page="add"]')) return;
    const isbnForm = document.getElementById("isbn-form");
    const searchForm = document.getElementById("search-form");
    const host = document.getElementById("add-results");
    if (!host) return;

    if (isbnForm) {
      isbnForm.addEventListener("submit", async (e) => {
        e.preventDefault();
        const isbnInput = isbnForm.querySelector('input[name="isbn"]');
        const isbn = isbnInput ? isbnInput.value.replace(/[\s-]/g, "") : "";
        if (!isbn) {
          showBanner(host, "error", "ISBN is required.");
          return;
        }
        host.textContent = "";
        const resp = await api("POST", "/api/add/lookup", { isbn });
        if (!resp.ok) {
          showBanner(host, resp.status === 404 ? "warn" : "error",
            errorText(resp.data, "Lookup failed."));
          return;
        }
        renderLookup(host, resp.data.metadata);
      });
    }

    if (searchForm) {
      searchForm.addEventListener("submit", async (e) => {
        e.preventDefault();
        const qInput = searchForm.querySelector('input[name="q"]');
        const q = qInput ? qInput.value.trim() : "";
        if (!q) {
          showBanner(host, "error", "Query is required.");
          return;
        }
        host.textContent = "";
        const resp = await api("POST", "/api/add/search", { q });
        if (!resp.ok) {
          showBanner(host, "error", errorText(resp.data, "Search failed."));
          return;
        }
        renderSearchResults(host, resp.data.results || []);
      });
    }
  }

  async function resolveCover(ref) {
    if (!ref) return "";
    const resp = await api("POST", "/api/add/cover", { ref });
    if (!resp.ok) return "";
    return (resp.data && resp.data.cover) || "";
  }

  function renderLookup(host, md) {
    host.textContent = "";
    const heading = document.createElement("h2");
    heading.textContent = "Preview";
    host.appendChild(heading);
    const card = makeResultCard({
      title: md.title || "",
      subtitle: md.subtitle || "",
      authors: md.authors || [],
      year: md.publish_date || "",
      isbn: md.isbn_10 || md.isbn_13 || "",
      publisher: md.publisher || "",
      totalPages: md.total_pages || null,
      coverRef: md.cover_ref || "",
      onAdd: () => submitCreate({
        title: md.title,
        subtitle: md.subtitle,
        authors: md.authors,
        publisher: md.publisher,
        publish_date: md.publish_date,
        total_pages: md.total_pages,
        isbn: md.isbn_10 || md.isbn_13,
        categories: md.categories,
        series: "",
        series_index: null,
        cover_ref: md.cover_ref,
      }, card),
    });
    host.appendChild(card);
  }

  function renderSearchResults(host, results) {
    host.textContent = "";
    const heading = document.createElement("h2");
    heading.textContent = "Results (" + results.length + ")";
    host.appendChild(heading);
    if (results.length === 0) {
      const p = document.createElement("p");
      p.innerHTML = "<em>No results.</em>";
      host.appendChild(p);
      return;
    }
    results.forEach((r) => {
      const card = makeResultCard({
        title: r.title || "",
        subtitle: "",
        authors: r.authors || [],
        year: r.publish_year || "",
        isbn: r.isbn || "",
        publisher: "",
        totalPages: null,
        coverRef: r.cover_ref || "",
        onAdd: () => {
          // If the result has an ISBN, prefer a full lookup so we write
          // richer frontmatter. Otherwise use what the search gave us.
          if (r.isbn) {
            lookupAndCreate(host, r.isbn, card, r.cover_ref);
          } else {
            submitCreate({
              title: r.title,
              authors: r.authors,
              publish_date: r.publish_year,
              cover_ref: r.cover_ref,
            }, card);
          }
        },
      });
      host.appendChild(card);
    });
  }

  async function lookupAndCreate(host, isbn, anchor, fallbackCoverRef) {
    const resp = await api("POST", "/api/add/lookup", { isbn });
    if (!resp.ok) {
      showBanner(anchor, resp.status === 404 ? "warn" : "error",
        errorText(resp.data, "Lookup failed."));
      return;
    }
    const md = resp.data.metadata;
    submitCreate({
      title: md.title,
      subtitle: md.subtitle,
      authors: md.authors,
      publisher: md.publisher,
      publish_date: md.publish_date,
      total_pages: md.total_pages,
      isbn: md.isbn_10 || md.isbn_13,
      categories: md.categories,
      cover_ref: md.cover_ref || fallbackCoverRef,
    }, anchor);
  }

  function makeResultCard(data) {
    const card = document.createElement("article");
    card.className = "add-result";

    const imgWrap = document.createElement("div");
    const img = document.createElement("img");
    img.alt = data.title ? ("Cover of " + data.title) : "Cover";
    imgWrap.appendChild(img);
    card.appendChild(imgWrap);

    const mid = document.createElement("div");
    const h = document.createElement("h3");
    h.textContent = data.title;
    mid.appendChild(h);
    if (data.subtitle) {
      const sub = document.createElement("p");
      sub.textContent = data.subtitle;
      mid.appendChild(sub);
    }
    if ((data.authors || []).length > 0) {
      const a = document.createElement("p");
      a.textContent = data.authors.join(", ");
      mid.appendChild(a);
    }
    const detailBits = [];
    if (data.year) detailBits.push(data.year);
    if (data.publisher) detailBits.push(data.publisher);
    if (data.isbn) detailBits.push("ISBN " + data.isbn);
    if (data.totalPages) detailBits.push(data.totalPages + " pages");
    if (detailBits.length > 0) {
      const d = document.createElement("p");
      d.textContent = detailBits.join(" · ");
      mid.appendChild(d);
    }
    card.appendChild(mid);

    const actions = document.createElement("div");
    actions.className = "add-actions";
    const addBtn = document.createElement("button");
    addBtn.type = "button";
    addBtn.className = "primary";
    addBtn.textContent = "Add this book";
    addBtn.addEventListener("click", () => {
      addBtn.disabled = true;
      Promise.resolve(data.onAdd()).catch(() => { addBtn.disabled = false; });
    });
    actions.appendChild(addBtn);
    card.appendChild(actions);

    // Cover preview fetched server-side into /covers/ so CSP stays 'self'.
    if (data.coverRef) {
      resolveCover(data.coverRef).then((href) => {
        if (href) img.src = href;
      });
    }
    return card;
  }

  async function submitCreate(body, anchor) {
    // The server ignores any cover field not already in our cache, so
    // fetch the cover first (if the caller gave us a ref) and pass the
    // resulting /covers/... path into the create payload.
    let cover = "";
    if (body.cover_ref) {
      cover = await resolveCover(body.cover_ref);
    }
    const payload = Object.assign({}, body, { cover });
    delete payload.cover_ref;
    const resp = await api("POST", "/api/add/create", payload);
    if (!resp.ok) {
      showBanner(anchor || document.body,
        resp.status === 409 ? "warn" : "error",
        errorText(resp.data, "Could not add book."));
      return;
    }
    if (resp.data && resp.data.url) {
      window.location.href = resp.data.url;
    } else {
      window.location.href = "/library";
    }
  }

  // Book detail: manual cover refresh.
  function initCoverControls() {
    const host = document.querySelector("[data-cover-controls]");
    if (!host) return;
    const btn = host.querySelector("#refresh-cover-btn");
    if (!btn) return;
    const filename = host.getAttribute("data-filename") || "";
    btn.addEventListener("click", () => withBusy(btn, async () => {
      const resp = await api(
        "POST",
        "/api/books/" + encodeURIComponent(filename) + "/cover",
        {},
      );
      if (!resp.ok) {
        toast(resp.status === 404 ? "warn" : "error",
          errorText(resp.data, "Could not refresh cover."));
        return;
      }
      toast("ok", "Cover saved. Reloading…");
      setTimeout(() => window.location.reload(), 600);
    }));
  }

  function registerServiceWorker() {
    if (!("serviceWorker" in navigator)) return;
    if (window.location.protocol !== "https:" &&
        window.location.hostname !== "localhost" &&
        window.location.hostname !== "127.0.0.1") {
      return;
    }
    navigator.serviceWorker.register("/sw.js", { scope: "/" }).catch(() => {
      // Registration failures are not actionable for the user; silent is fine.
    });
  }

  // initShortcuts wires document-level keyboard shortcuts:
  //   /            — focus the first filter/search input on the page
  //   g then l|s|a|i (within 600 ms) — navigate to /library, /series, /add, /import
  //   ?            — toggle the help overlay
  //   Esc          — close the overlay, or blur the current input
  //
  // Keys are ignored while typing in an input/textarea/select or in a
  // contenteditable element so the shortcuts never fight text entry.
  function initShortcuts() {
    const NAV_CHORD = { l: "/library", s: "/series", a: "/add", i: "/import" };
    const CHORD_TIMEOUT_MS = 600;
    const overlay = document.getElementById("kbd-help");
    const openBtn = document.getElementById("kbd-help-btn");

    let chordPending = false;
    let chordTimer = null;

    function isTyping(t) {
      if (!t) return false;
      if (t.isContentEditable) return true;
      const tag = t.tagName;
      return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";
    }

    function overlayOpen() { return overlay && !overlay.hidden; }
    function showOverlay() { if (overlay) overlay.hidden = false; }
    function hideOverlay() { if (overlay) overlay.hidden = true; }
    function toggleOverlay() { if (overlayOpen()) hideOverlay(); else showOverlay(); }

    function cancelChord() {
      chordPending = false;
      if (chordTimer) { clearTimeout(chordTimer); chordTimer = null; }
    }

    function focusFilter() {
      const sel = document.querySelector(
        ".filter-bar input, #isbn-form input[name=isbn], #search-form input[name=q]",
      );
      if (!sel) return false;
      sel.focus();
      if (typeof sel.select === "function") sel.select();
      return true;
    }

    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape") {
        if (overlayOpen()) { e.preventDefault(); hideOverlay(); return; }
        if (isTyping(document.activeElement) && document.activeElement.blur) {
          document.activeElement.blur();
        }
        cancelChord();
        return;
      }

      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (isTyping(e.target)) return;

      if (chordPending) {
        const dest = NAV_CHORD[e.key.toLowerCase()];
        cancelChord();
        if (dest) {
          e.preventDefault();
          window.location.href = dest;
        }
        return;
      }

      if (e.key === "/") {
        if (focusFilter()) e.preventDefault();
        return;
      }
      if (e.key === "?") {
        e.preventDefault();
        toggleOverlay();
        return;
      }
      if (e.key === "g") {
        chordPending = true;
        chordTimer = setTimeout(cancelChord, CHORD_TIMEOUT_MS);
      }
    });

    if (overlay) {
      overlay.querySelectorAll("[data-kbd-help-dismiss]").forEach((el) => {
        el.addEventListener("click", hideOverlay);
      });
    }
    if (openBtn) {
      openBtn.addEventListener("click", toggleOverlay);
    }
  }

  // Bar-chart width-in animation. Stats bars are server-rendered at their
  // final .bar--wN class so no-JS readers and reduced-motion users see the
  // correct width immediately. When motion is allowed, we strip the target
  // class, force a reflow to commit the 0%-width style, then restore the
  // target inside requestAnimationFrame — the existing CSS `transition:
  // width` rule does the tween. No-op when `.bar` is absent (every page
  // except /stats).
  function initBarAnimation() {
    if (window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      return;
    }
    var bars = document.querySelectorAll(".bar");
    if (bars.length === 0) return;
    var pending = [];
    bars.forEach(function (el) {
      var target = null;
      for (var i = 0; i < el.classList.length; i++) {
        var cls = el.classList[i];
        var m = cls.match(/^bar--w(\d+)$/);
        if (m && cls !== "bar--w0") {
          target = cls;
          break;
        }
      }
      if (!target) return;
      el.classList.remove(target);
      el.classList.add("bar--w0");
      pending.push({ el: el, target: target });
    });
    if (pending.length === 0) return;
    // Force a reflow so the 0% width paints before we schedule the target.
    // Reading offsetWidth on the last element is enough to flush layout.
    // eslint-disable-next-line no-unused-expressions
    pending[pending.length - 1].el.offsetWidth;
    requestAnimationFrame(function () {
      pending.forEach(function (p) {
        p.el.classList.remove("bar--w0");
        p.el.classList.add(p.target);
      });
    });
  }

  // initLibrarySearch wires the client-side filter on /library. The
  // server renders every book-card with a precomputed data-search="…"
  // lowercase haystack (title + subtitle + series + authors); we just
  // toggle visibility based on whether dataset.search includes the
  // current query substring. O(N) per keystroke, which is fine at the
  // library sizes we target (hundreds of books — sub-millisecond).
  //
  // State lives entirely in the DOM: `hidden` attribute on cards, a
  // live-region count, and a dedicated empty-state when the filter
  // returns zero matches. No .style.* (CSP).
  function initLibrarySearch() {
    const input = document.getElementById("library-search");
    const grid = document.getElementById("book-grid");
    if (!input || !grid) return;

    const cards = grid.querySelectorAll(".book-card");
    if (cards.length === 0) return;

    const count = document.getElementById("search-count");
    const countNum = document.getElementById("search-count-num");
    const countPlural = document.getElementById("search-count-plural");
    const empty = document.getElementById("search-empty");
    const emptyQ = document.getElementById("search-empty-q");
    const clearBtn = document.getElementById("search-clear-btn");

    function apply() {
      const q = input.value.trim().toLowerCase();
      let visible = 0;
      cards.forEach((card) => {
        const hay = card.dataset.search || "";
        const match = q === "" || hay.includes(q);
        card.hidden = !match;
        if (match) visible += 1;
      });
      if (q === "") {
        if (count) count.hidden = true;
        if (empty) empty.hidden = true;
      } else {
        if (count) {
          count.hidden = false;
          if (countNum) countNum.textContent = String(visible);
          if (countPlural) countPlural.textContent = visible === 1 ? "" : "s";
        }
        if (empty) {
          empty.hidden = visible !== 0;
          if (visible === 0 && emptyQ) emptyQ.textContent = q;
        }
      }
    }

    input.addEventListener("input", apply);
    if (clearBtn) {
      clearBtn.addEventListener("click", () => {
        input.value = "";
        input.focus();
        apply();
      });
    }

    // Respect a server-seeded ?q= on first render: apply the filter once
    // so the count + empty-state match the initial input.
    if (input.value.trim() !== "") apply();
  }

  document.addEventListener("DOMContentLoaded", () => {
    document.querySelectorAll("[data-rating-grid]").forEach(initRatingGrid);
    document.querySelectorAll("[data-status-select]").forEach(initStatus);
    document.querySelectorAll("[data-review-form]").forEach(initReview);
    initImport();
    initSync();
    initAddPage();
    initCoverControls();
    initLibrarySearch();
    initShortcuts();
    initBarAnimation();
    registerServiceWorker();
  });
})();
