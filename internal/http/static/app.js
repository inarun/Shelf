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

  function initRating(widget) {
    const filename = widget.getAttribute("data-filename");
    const buttons = widget.querySelectorAll("button[data-rating]");
    buttons.forEach((btn) => {
      btn.addEventListener("click", () => withBusy(btn, async () => {
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
          toast("ok", next === null ? "Rating cleared" : "Rated " + next + (next === 1 ? " star" : " stars"));
        } else if (resp.status === 409) {
          toast("warn", errorText(resp.data, "This note changed outside Shelf. Reload and try again."));
        } else {
          toast("error", errorText(resp.data, "Could not save rating."));
        }
      }));
    });
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

  document.addEventListener("DOMContentLoaded", () => {
    document.querySelectorAll("[data-rating-widget]").forEach(initRating);
    document.querySelectorAll("[data-status-select]").forEach(initStatus);
    document.querySelectorAll("[data-review-form]").forEach(initReview);
    initImport();
    initAddPage();
    initCoverControls();
    registerServiceWorker();
  });
})();
