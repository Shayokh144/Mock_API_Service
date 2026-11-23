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
    var open = openKeys();
    for (var o = 0; o < open.length; o++) {
      kept.append("open", open[o]);
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
      var open = openKeys();
      for (var o = 0; o < open.length; o++) {
        kept.append("open", open[o]);
      }
      var href = kept.toString();
      links[i].setAttribute("href", "/_admin/history" + (href ? "?" + href : ""));
    }
    refreshToggleLinks();
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

  function badge(className, text) {
    var span = document.createElement("span");
    span.className = className;
    span.textContent = text;
    return span;
  }

  function statusClassName(text) {
    var code = parseInt(text, 10);
    if (code >= 200 && code < 300) {
      return "badge status-2xx";
    }
    if (code >= 300 && code < 400) {
      return "badge status-3xx";
    }
    if (code >= 400 && code < 500) {
      return "badge status-4xx";
    }
    if (code >= 500 && code < 600) {
      return "badge status-5xx";
    }
    return "badge status-rejected";
  }

  function sourceClassName(text) {
    if (text === "mock") {
      return "badge badge-mock";
    }
    if (text.indexOf("upstream-error") === 0) {
      return "badge status-5xx";
    }
    if (text === "rejected" || text.indexOf("fault") === 0) {
      return "badge badge-rejected";
    }
    return "badge badge-neutral";
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
    table.className = "history";
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
    var statusCell = document.createElement("td");
    statusCell.appendChild(badge(statusClassName(String(entry.status)), String(entry.status)));
    tr.appendChild(statusCell);
    var sourceCell = document.createElement("td");
    sourceCell.appendChild(badge(sourceClassName(entry.source || ""), entry.source || ""));
    tr.appendChild(sourceCell);
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
    tr.setAttribute("data-open", "0");
    tr.appendChild(cell(entry.method || ""));
    var urlCell = document.createElement("td");
    var link = document.createElement("a");
    link.className = "history-toggle";
    link.href = "/_admin/history";
    link.setAttribute("aria-expanded", "false");
    link.appendChild(field("arrow", "\u25b6"));
    var path = field("path", entry.path || "");
    path.className = "history-path";
    link.appendChild(path);
    var count = field("count", "");
    count.className = "history-count";
    link.appendChild(count);
    urlCell.appendChild(link);
    tr.appendChild(urlCell);
    tr.appendChild(namedCell("status", ""));
    tr.appendChild(namedCell("sources", ""));
    tr.appendChild(namedCell("upstream", entry.upstream || ""));
    tr.appendChild(namedCell("delay", ""));
    tr.appendChild(namedCell("rule", ""));
    tr.appendChild(namedCell("when", entry.when || ""));
    tr.appendChild(cell(""));
    return tr;
  }

  function namedCell(name, text) {
    var td = cell(text);
    td.setAttribute("data-field", name);
    return td;
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

  function membersOf(group) {
    var key = group.getAttribute("data-group");
    var list = [];
    var child = group.nextElementSibling;
    while (child && child.getAttribute("data-kind") !== "group" && child.getAttribute("data-group") === key) {
      list.push(child);
      child = child.nextElementSibling;
    }
    return list;
  }

  function matchedMembers(group) {
    var members = membersOf(group);
    var matched = [];
    for (var i = 0; i < members.length; i++) {
      if (members[i].getAttribute("data-match") !== "0") {
        matched.push(members[i]);
      }
    }
    return matched;
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

  function setLines(group, name, lines) {
    var nodes = group.querySelectorAll("[data-field]");
    var cell = null;
    for (var i = 0; i < nodes.length; i++) {
      if (nodes[i].getAttribute("data-field") === name) {
        cell = nodes[i];
        break;
      }
    }
    if (!cell) {
      return;
    }
    while (cell.firstChild) {
      cell.removeChild(cell.firstChild);
    }
    for (var n = 0; n < lines.length; n++) {
      if (n) {
        cell.appendChild(document.createElement("br"));
      }
      if (name === "status") {
        cell.appendChild(badge(statusClassName(lines[n]), lines[n]));
      } else if (name === "sources") {
        cell.appendChild(badge(sourceClassName(lines[n]), lines[n]));
      } else {
        cell.appendChild(document.createTextNode(lines[n]));
      }
    }
  }

  function sharedColumn(rowsInGroup, index) {
    if (!rowsInGroup.length) {
      return "";
    }
    var cells = rowsInGroup[0].querySelectorAll("td");
    var first = cells[index] ? cells[index].textContent : "";
    for (var i = 1; i < rowsInGroup.length; i++) {
      var next = rowsInGroup[i].querySelectorAll("td");
      var text = next[index] ? next[index].textContent : "";
      if (text !== first) {
        return "";
      }
    }
    if (index === 6 && first === "none") {
      return "";
    }
    return first;
  }

  function refreshSummary(group, members) {
    if (!members) {
      members = matchedMembers(group);
    }
    if (!members.length) {
      return;
    }
    var count = members.length;
    setField(group, "count", count === 1 ? "1 request" : count + " requests");
    setLines(group, "status", statusText(members).split(", "));
    setLines(group, "sources", sourceText(members).split(", "));
    var first = members[0].querySelectorAll("td");
    setField(group, "when", first[7] ? first[7].textContent : "");
    setField(group, "upstream", first[4] ? first[4].textContent : "");
    setField(group, "delay", sharedColumn(members, 5));
    setField(group, "rule", sharedColumn(members, 6));
  }

  function paintChildren(group) {
    var open = group.getAttribute("data-open") === "1";
    var members = membersOf(group);
    for (var i = 0; i < members.length; i++) {
      var match = members[i].getAttribute("data-match") !== "0";
      members[i].hidden = !(open && match);
    }
  }

  function findSummary(body, key) {
    for (var i = 0; i < body.children.length; i++) {
      var node = body.children[i];
      if (node.getAttribute("data-kind") === "group" && node.getAttribute("data-group") === key) {
        return node;
      }
    }
    return null;
  }

  function findSingle(body, key) {
    for (var i = 0; i < body.children.length; i++) {
      var node = body.children[i];
      if (node.getAttribute("data-kind") !== "group" && node.getAttribute("data-id") && node.getAttribute("data-group") === key && node.className.indexOf("history-child") === -1) {
        return node;
      }
    }
    return null;
  }

  function moveGroupToTop(summary) {
    var body = summary.parentNode;
    if (!body || body.firstChild === summary) {
      return;
    }
    var key = summary.getAttribute("data-group");
    var nodes = [summary];
    var child = summary.nextElementSibling;
    while (child && child.getAttribute("data-kind") !== "group" && child.getAttribute("data-group") === key) {
      nodes.push(child);
      child = child.nextElementSibling;
    }
    var anchor = body.firstChild;
    for (var i = 0; i < nodes.length; i++) {
      body.insertBefore(nodes[i], anchor);
    }
  }

  function place(body, entry, query) {
    var row = buildRow(entry, query);
    if (!groupingOn()) {
      body.insertBefore(row, body.firstChild || null);
      return;
    }
    var key = entry.group_key || "";
    var summary = key ? findSummary(body, key) : null;
    if (summary) {
      row.className = "history-child";
      row.hidden = summary.getAttribute("data-open") !== "1";
      body.insertBefore(row, summary.nextSibling);
      refreshSummary(summary, membersOf(summary));
      moveGroupToTop(summary);
      return;
    }
    var single = key ? findSingle(body, key) : null;
    if (single) {
      var created = buildGroup(entry);
      row.className = "history-child";
      row.hidden = true;
      single.className = "history-child";
      single.hidden = true;
      body.insertBefore(created, single);
      body.insertBefore(row, single);
      refreshSummary(created, membersOf(created));
      moveGroupToTop(created);
      return;
    }
    body.insertBefore(row, body.firstChild || null);
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
      list[i].setAttribute("data-match", rowMatches(list[i], needle) ? "1" : "0");
    }
    var shown = 0;
    var groups = 0;
    for (var n = 0; n < rows.children.length; n++) {
      var child = rows.children[n];
      if (child.getAttribute("data-kind") === "group") {
        var members = matchedMembers(child);
        child.hidden = members.length === 0;
        paintChildren(child);
        if (members.length) {
          refreshSummary(child, members);
          groups++;
          shown += members.length;
        }
      } else if (child.getAttribute("data-id") && child.className.indexOf("history-child") === -1) {
        child.hidden = child.getAttribute("data-match") === "0";
        if (!child.hidden) {
          shown++;
          groups++;
        }
      }
    }
    return { shown: shown, groups: groups };
  }

  function openKeys() {
    var keys = [];
    if (!rows) {
      return keys;
    }
    var groups = rows.querySelectorAll('tr[data-kind="group"]');
    for (var i = 0; i < groups.length; i++) {
      if (groups[i].getAttribute("data-open") === "1") {
        keys.push(groups[i].getAttribute("data-group") || "");
      }
    }
    return keys;
  }

  function pageQuery(open) {
    var params = new URLSearchParams(window.location.search);
    var kept = new URLSearchParams();
    var text = currentText();
    var source = params.get("source");
    var upstream = params.get("upstream");
    var session = params.get("session");
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
    for (var i = 0; i < open.length; i++) {
      if (open[i]) {
        kept.append("open", open[i]);
      }
    }
    var encoded = kept.toString();
    return encoded ? "?" + encoded : "";
  }

  function refreshToggleLinks() {
    if (!rows) {
      return;
    }
    var current = openKeys();
    var groups = rows.querySelectorAll('tr[data-kind="group"]');
    for (var i = 0; i < groups.length; i++) {
      var key = groups[i].getAttribute("data-group") || "";
      var next = [];
      for (var n = 0; n < current.length; n++) {
        if (current[n] !== key) {
          next.push(current[n]);
        }
      }
      if (groups[i].getAttribute("data-open") !== "1") {
        next.push(key);
      }
      var link = groups[i].querySelector("a.history-toggle");
      if (link) {
        link.setAttribute("href", "/_admin/history" + pageQuery(next));
      }
    }
  }

  function groupFrom(node) {
    while (node && node !== document) {
      if (node.getAttribute && node.getAttribute("data-kind") === "group") {
        return node;
      }
      node = node.parentNode;
    }
    return null;
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

  document.addEventListener("click", function (event) {
    if (!rows || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }
    var group = groupFrom(event.target);
    if (!group || !rows.contains(group)) {
      return;
    }
    event.preventDefault();
    var open = group.getAttribute("data-open") !== "1";
    group.setAttribute("data-open", open ? "1" : "0");
    setField(group, "arrow", open ? "\u25bc" : "\u25b6");
    var link = group.querySelector("a.history-toggle");
    if (link) {
      link.setAttribute("aria-expanded", open ? "true" : "false");
    }
    paintChildren(group);
    syncAddress();
  });

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
