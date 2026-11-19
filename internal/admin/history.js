(function () {
  var summary = document.getElementById("history-summary");
  var upstreamSummary = document.getElementById("history-upstream");
  var search = document.getElementById("history-text-filter");
  var groupSelect = document.getElementById("history-group");
  var rows = document.getElementById("history-rows");
  var empty = document.getElementById("history-empty");
  if (!summary) {
    return;
  }

  var headers = ["Method", "URL", "Status", "Source", "Upstream", "Delay", "Rule", "Timestamp", "Actions"];

  function groupingOn() {
    return summary.getAttribute("data-grouped") === "1";
  }

  function readOnly() {
    return summary.getAttribute("data-readonly") === "1";
  }

  function currentText() {
    if (!search) {
      return "";
    }
    return search.value.replace(/^\s+|\s+$/g, "");
  }

  function groupOff() {
    if (groupSelect) {
      return groupSelect.value === "0";
    }
    var params = new URLSearchParams(window.location.search);
    return params.get("group") === "0";
  }

  function filterQuery() {
    var params = new URLSearchParams(window.location.search);
    var kept = new URLSearchParams();
    var source = params.get("source");
    var upstream = params.get("upstream");
    var session = params.get("session");
    var text = currentText();
    if (text) {
      kept.set("q", text);
    }
    if (session && session !== "default") {
      kept.set("session", session);
    }
    if (source) {
      kept.set("source", source);
    }
    if (upstream) {
      kept.set("upstream", upstream);
    }
    if (groupOff()) {
      kept.set("group", "0");
    }
    var encoded = kept.toString();
    if (!encoded) {
      return "";
    }
    return "?" + encoded;
  }

  function summaryLine(label, text, shown, total, groups) {
    var line = "Filter: " + label + ".";
    if (text) {
      line += " Text: " + text + ".";
    }
    if (groupingOn()) {
      var noun = groups === 1 ? "group" : "groups";
      return line + " Showing " + shown + " of " + total + " requests in " + groups + " " + noun + ".";
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
    if (groupOff()) {
      params.set("group", "0");
    } else {
      params.delete("group");
    }
    var encoded = params.toString();
    var next = window.location.pathname + (encoded ? "?" + encoded : "");
    if (window.history && window.history.replaceState) {
      window.history.replaceState(null, "", next);
    }
    var links = document.querySelectorAll('nav[aria-label="History source"] a');
    var upstream = params.get("upstream") || "";
    var session = params.get("session") || "";
    for (var i = 0; i < links.length; i++) {
      var source = links[i].getAttribute("data-source") || "";
      var kept = new URLSearchParams();
      if (text) {
        kept.set("q", text);
      }
      if (session && session !== "default") {
        kept.set("session", session);
      }
      if (source) {
        kept.set("source", source);
      }
      if (upstream) {
        kept.set("upstream", upstream);
      }
      if (groupOff()) {
        kept.set("group", "0");
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

  function ruleText(entry) {
    if (!entry.rule_id) {
      return "none";
    }
    return (entry.rule_name || "unnamed") + " (" + entry.rule_id + ")";
  }

  function cell(text) {
    var td = document.createElement("td");
    td.textContent = text;
    return td;
  }

  function headerRow() {
    var headRow = document.createElement("tr");
    headers.forEach(function (label) {
      var th = document.createElement("th");
      th.scope = "col";
      th.textContent = label;
      headRow.appendChild(th);
    });
    return headRow;
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
    head.appendChild(headerRow());
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
    if (entry.group_key) {
      tr.setAttribute("data-group", entry.group_key);
    }
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
    tr.appendChild(cell(ruleText(entry)));
    tr.appendChild(cell(entry.when));
    var action = document.createElement("td");
    if (!readOnly()) {
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
    }
    tr.appendChild(action);
    return tr;
  }

  function field(name, text) {
    var span = document.createElement("span");
    span.setAttribute("data-field", name);
    span.textContent = text;
    return span;
  }

  function buildGroup(entry) {
    var tr = document.createElement("tr");
    tr.className = "history-group";
    tr.setAttribute("data-kind", "group");
    tr.setAttribute("data-group", entry.group_key || "");
    var td = document.createElement("td");
    td.colSpan = 9;
    var details = document.createElement("details");
    var groupSummary = document.createElement("summary");
    groupSummary.appendChild(field("method", entry.method || ""));
    groupSummary.appendChild(field("path", entry.path || ""));
    groupSummary.appendChild(field("upstream", entry.upstream || ""));
    groupSummary.appendChild(field("when", entry.when || ""));
    groupSummary.appendChild(field("count", ""));
    groupSummary.appendChild(field("status", ""));
    groupSummary.appendChild(field("sources", ""));
    details.appendChild(groupSummary);
    var table = document.createElement("table");
    var head = document.createElement("thead");
    head.appendChild(headerRow());
    table.appendChild(head);
    var body = document.createElement("tbody");
    table.appendChild(body);
    details.appendChild(table);
    td.appendChild(details);
    tr.appendChild(td);
    return tr;
  }

  function setField(group, name, text) {
    var nodes = group.querySelectorAll("[data-field]");
    for (var i = 0; i < nodes.length; i++) {
      if (nodes[i].getAttribute("data-field") === name) {
        nodes[i].textContent = text;
        return;
      }
    }
  }

  function visibleMembers(group) {
    var members = group.querySelectorAll("tr[data-id]");
    var visible = [];
    for (var i = 0; i < members.length; i++) {
      if (!members[i].hidden) {
        visible.push(members[i]);
      }
    }
    return visible;
  }

  function statusText(rowsInGroup) {
    var counts = {};
    var order = [];
    for (var i = 0; i < rowsInGroup.length; i++) {
      var cells = rowsInGroup[i].querySelectorAll("td");
      var status = cells[2] ? cells[2].textContent : "";
      if (counts[status] === undefined) {
        order.push(status);
        counts[status] = 0;
      }
      counts[status]++;
    }
    order.sort(function (a, b) {
      if (counts[a] !== counts[b]) {
        return counts[b] - counts[a];
      }
      return Number(a) - Number(b);
    });
    var parts = [];
    for (var n = 0; n < order.length; n++) {
      parts.push(order[n] + " \u00d7" + counts[order[n]]);
    }
    return parts.join(", ");
  }

  function sourceText(rowsInGroup) {
    var seen = {};
    var parts = [];
    for (var i = 0; i < rowsInGroup.length; i++) {
      var cells = rowsInGroup[i].querySelectorAll("td");
      var source = cells[3] ? cells[3].textContent : "";
      if (seen[source]) {
        continue;
      }
      seen[source] = true;
      parts.push(source);
    }
    return parts.join(", ");
  }

  function refreshSummary(group, members) {
    if (!members) {
      members = visibleMembers(group);
    }
    if (!members.length) {
      return;
    }
    var count = members.length;
    setField(group, "count", count === 1 ? "1 request" : count + " requests");
    setField(group, "status", statusText(members));
    setField(group, "sources", sourceText(members));
    var first = members[0].querySelectorAll("td");
    setField(group, "when", first[7] ? first[7].textContent : "");
    setField(group, "upstream", first[4] ? first[4].textContent : "");
  }

  function findDirect(body, key) {
    for (var i = 0; i < body.children.length; i++) {
      if (body.children[i].getAttribute("data-group") === key) {
        return body.children[i];
      }
    }
    return null;
  }

  function place(body, entry, query) {
    var row = buildRow(entry, query);
    if (!groupingOn()) {
      body.insertBefore(row, body.firstChild || null);
      return;
    }
    var key = entry.group_key || "";
    var existing = key ? findDirect(body, key) : null;
    if (!existing) {
      body.insertBefore(row, body.firstChild || null);
      return;
    }
    if (existing.getAttribute("data-kind") !== "group") {
      var created = buildGroup(entry);
      var createdBody = created.querySelector("tbody");
      body.replaceChild(created, existing);
      createdBody.appendChild(row);
      createdBody.appendChild(existing);
      refreshSummary(created);
      body.insertBefore(created, body.firstChild || null);
      return;
    }
    var inner = existing.querySelector("tbody");
    if (inner) {
      inner.insertBefore(row, inner.firstChild || null);
    }
    refreshSummary(existing);
    body.insertBefore(existing, body.firstChild || null);
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
    for (var n = fresh.length - 1; n >= 0; n--) {
      place(body, fresh[n], query);
    }
  }

  function rowMatches(row, needle) {
    var link = row.querySelector("a");
    var url = link ? link.textContent : "";
    return !needle || url.toLowerCase().indexOf(needle) !== -1;
  }

  function applyLocal(text) {
    if (!rows) {
      return { shown: 0, groups: 0 };
    }
    var needle = text.toLowerCase();
    var list = rows.querySelectorAll("tr[data-id]");
    for (var i = 0; i < list.length; i++) {
      list[i].hidden = !rowMatches(list[i], needle);
    }
    var shown = 0;
    var groups = 0;
    for (var n = 0; n < rows.children.length; n++) {
      var child = rows.children[n];
      if (child.getAttribute("data-kind") === "group") {
        var members = visibleMembers(child);
        child.hidden = members.length === 0;
        if (members.length) {
          refreshSummary(child, members);
          groups++;
          shown += members.length;
        }
      } else if (child.getAttribute("data-id")) {
        if (!child.hidden) {
          shown++;
          groups++;
        }
      }
    }
    return { shown: shown, groups: groups };
  }

  function writeSummary(label, text, shown, total, groups) {
    summary.textContent = summaryLine(label, text, shown, total, groups);
    summary.setAttribute("data-label", label || "");
    summary.setAttribute("data-total", String(total));
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
      if (upstreamSummary) {
        upstreamSummary.textContent = "Upstream: " + (data.upstream_label || "All upstreams") + ".";
      }
      addNew(data.entries, query);
      var counted = applyLocal(currentText());
      writeSummary(data.label || "", data.text || currentText(), counted.shown, data.total, counted.groups);
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
      var counted = applyLocal(text);
      writeSummary(summary.getAttribute("data-label") || "All", text, counted.shown, summary.getAttribute("data-total") || "0", counted.groups);
      syncAddress();
      tick();
    });
  }

  if (groupSelect) {
    groupSelect.addEventListener("change", function () {
      var params = new URLSearchParams(window.location.search);
      if (groupSelect.value === "0") {
        params.set("group", "0");
      } else {
        params.delete("group");
      }
      var encoded = params.toString();
      window.location.assign(window.location.pathname + (encoded ? "?" + encoded : ""));
    });
  }

  window.setInterval(tick, 1000);
})();
