(function () {
  var summary = document.getElementById("history-summary");
  var upstreamSummary = document.getElementById("history-upstream");
  var search = document.getElementById("history-text-filter");
  var rows = document.getElementById("history-rows");
  var empty = document.getElementById("history-empty");
  if (!summary) {
    return;
  }

  function currentText() {
    if (!search) {
      return "";
    }
    return search.value.replace(/^\s+|\s+$/g, "");
  }

  function filterQuery() {
    var params = new URLSearchParams(window.location.search);
    var kept = new URLSearchParams();
    var source = params.get("source");
    var upstream = params.get("upstream");
    var text = currentText();
    if (text) {
      kept.set("q", text);
    }
    if (source) {
      kept.set("source", source);
    }
    if (upstream) {
      kept.set("upstream", upstream);
    }
    var encoded = kept.toString();
    if (!encoded) {
      return "";
    }
    return "?" + encoded;
  }

  function summaryLine(label, text, shown, total) {
    var line = "Filter: " + label + ".";
    if (text) {
      line += " Text: " + text + ".";
    }
    return line + " Showing " + shown + " of " + total + ".";
  }

  function syncAddress() {
    var params = new URLSearchParams(window.location.search);
    var text = currentText();
    if (text) {
      params.set("q", text);
    } else {
      params.delete("q");
    }
    var encoded = params.toString();
    var next = window.location.pathname + (encoded ? "?" + encoded : "");
    if (window.history && window.history.replaceState) {
      window.history.replaceState(null, "", next);
    }
    var links = document.querySelectorAll('nav[aria-label="History source"] a');
    var upstream = params.get("upstream") || "";
    for (var i = 0; i < links.length; i++) {
      var source = links[i].getAttribute("data-source") || "";
      var kept = new URLSearchParams();
      if (text) {
        kept.set("q", text);
      }
      if (source) {
        kept.set("source", source);
      }
      if (upstream) {
        kept.set("upstream", upstream);
      }
      var href = kept.toString();
      links[i].setAttribute("href", "/_admin/history" + (href ? "?" + href : ""));
    }
    var clearHref = "/_admin/history/clear" + filterQuery();
    var clearLink = document.getElementById("history-clear");
    if (clearLink) {
      clearLink.setAttribute("href", clearHref);
    }
    var clearForm = document.getElementById("history-clear-form");
    if (clearForm) {
      clearForm.setAttribute("action", clearHref);
    }
  }

  function cell(text) {
    var td = document.createElement("td");
    td.textContent = text;
    return td;
  }

  function ensureRows() {
    if (rows) {
      return rows;
    }
    var table = document.createElement("table");
    var caption = document.createElement("caption");
    caption.textContent = "Recorded requests";
    table.appendChild(caption);
    var head = document.createElement("thead");
    var headRow = document.createElement("tr");
    ["Method", "URL", "Status", "Source", "Upstream", "Delay", "Timestamp", "Actions"].forEach(function (label) {
      var th = document.createElement("th");
      th.scope = "col";
      th.textContent = label;
      headRow.appendChild(th);
    });
    head.appendChild(headRow);
    table.appendChild(head);
    rows = document.createElement("tbody");
    rows.id = "history-rows";
    table.appendChild(rows);
    if (empty && empty.parentNode) {
      empty.parentNode.insertBefore(table, empty);
      empty.parentNode.removeChild(empty);
      empty = null;
    } else {
      summary.parentNode.insertBefore(table, summary.nextSibling);
    }
    return rows;
  }

  function buildRow(entry, query) {
    var tr = document.createElement("tr");
    tr.setAttribute("data-id", entry.id);
    tr.appendChild(cell(entry.method));
    var linkCell = document.createElement("td");
    var link = document.createElement("a");
    link.href = "/_admin/history/" + encodeURIComponent(entry.id) + query;
    link.textContent = entry.url;
    linkCell.appendChild(link);
    tr.appendChild(linkCell);
    tr.appendChild(cell(String(entry.status)));
    tr.appendChild(cell(entry.source));
    tr.appendChild(cell(entry.upstream || ""));
    tr.appendChild(cell(entry.delay || ""));
    tr.appendChild(cell(entry.when));
    var action = document.createElement("td");
    var actions = document.createElement("div");
    actions.className = "actions";
    var create = document.createElement("a");
    create.href = "/_admin/rules/new?from=" + encodeURIComponent(entry.id);
    create.textContent = "Create rule";
    actions.appendChild(create);
    var form = document.createElement("form");
    form.className = "inline";
    form.method = "post";
    form.action = "/_admin/history/" + encodeURIComponent(entry.id) + "/delete" + query;
    var button = document.createElement("button");
    button.className = "danger";
    button.type = "submit";
    button.textContent = "Delete";
    form.appendChild(button);
    actions.appendChild(form);
    action.appendChild(actions);
    tr.appendChild(action);
    return tr;
  }

  function knownIds(body) {
    var seen = {};
    var existing = body.querySelectorAll("tr[data-id]");
    for (var i = 0; i < existing.length; i++) {
      seen[existing[i].getAttribute("data-id")] = true;
    }
    return seen;
  }

  function addNew(entries, query) {
    var body = ensureRows();
    var seen = knownIds(body);
    var fresh = [];
    for (var i = 0; i < entries.length; i++) {
      if (!seen[entries[i].id]) {
        fresh.push(entries[i]);
      }
    }
    for (var n = 0; n < fresh.length; n++) {
      var before = body.children[n] || null;
      body.insertBefore(buildRow(fresh[n], query), before);
    }
  }

  function applyLocal(text) {
    if (!rows) {
      return 0;
    }
    var needle = text.toLowerCase();
    var list = rows.querySelectorAll("tr[data-id]");
    var visible = 0;
    for (var i = 0; i < list.length; i++) {
      var link = list[i].querySelector("a");
      var url = link ? link.textContent : "";
      var show = !needle || url.toLowerCase().indexOf(needle) !== -1;
      list[i].hidden = !show;
      if (show) {
        visible++;
      }
    }
    return visible;
  }

  function tick() {
    var query = filterQuery();
    var request = new XMLHttpRequest();
    request.open("GET", "/_admin/history/feed" + query, true);
    request.setRequestHeader("Accept", "application/json");
    request.onreadystatechange = function () {
      if (request.readyState !== 4 || request.status !== 200) {
        return;
      }
      var data;
      try {
        data = JSON.parse(request.responseText);
      } catch (err) {
        return;
      }
      if (!data || !data.entries) {
        return;
      }
      summary.textContent = summaryLine(data.label, data.text || "", data.shown, data.total);
      summary.setAttribute("data-label", data.label || "");
      summary.setAttribute("data-total", String(data.total));
      if (upstreamSummary) {
        upstreamSummary.textContent = "Upstream: " + (data.upstream_label || "All upstreams") + ".";
      }
      addNew(data.entries, query);
      applyLocal(currentText());
    };
    request.send();
  }

  var clearLink = document.getElementById("history-clear");
  var clearDialog = document.getElementById("history-clear-dialog");
  var clearCancel = document.getElementById("history-clear-cancel");
  if (clearLink && clearDialog && typeof clearDialog.showModal === "function") {
    clearLink.addEventListener("click", function (event) {
      event.preventDefault();
      if (!clearDialog.open) {
        clearDialog.showModal();
      }
    });
    if (clearCancel) {
      clearCancel.addEventListener("click", function () {
        clearDialog.close();
      });
    }
    clearDialog.addEventListener("click", function (event) {
      if (event.target === clearDialog) {
        clearDialog.close();
      }
    });
  }

  if (search) {
    search.addEventListener("input", function () {
      var text = currentText();
      var visible = applyLocal(text);
      summary.textContent = summaryLine(summary.getAttribute("data-label") || "All", text, visible, summary.getAttribute("data-total") || "0");
      syncAddress();
      tick();
    });
  }

  window.setInterval(tick, 1000);
})();
