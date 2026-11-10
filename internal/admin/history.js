(function () {
  var summary = document.getElementById("history-summary");
  var upstreamSummary = document.getElementById("history-upstream");
  var rows = document.getElementById("history-rows");
  var empty = document.getElementById("history-empty");
  if (!summary) {
    return;
  }

  function filterQuery() {
    var params = new URLSearchParams(window.location.search);
    var kept = new URLSearchParams();
    var source = params.get("source");
    var upstream = params.get("upstream");
    if (source) {
      kept.set("source", source);
    }
    if (upstream) {
      kept.set("upstream", upstream);
    }
    var text = kept.toString();
    if (!text) {
      return "";
    }
    return "?" + text;
  }

  var query = filterQuery();

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

  function buildRow(entry) {
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
    var form = document.createElement("form");
    form.method = "post";
    form.action = "/_admin/history/" + encodeURIComponent(entry.id) + "/delete" + query;
    var button = document.createElement("button");
    button.className = "danger";
    button.type = "submit";
    button.textContent = "Delete";
    form.appendChild(button);
    action.appendChild(form);
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

  function addNew(entries) {
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
      body.insertBefore(buildRow(fresh[n]), before);
    }
  }

  function tick() {
    var request = new XMLHttpRequest();
    request.open("GET", "/_admin/history/feed" + filterQuery(), true);
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
      summary.textContent = "Filter: " + data.label + ". Showing " + data.shown + " of " + data.total + ".";
      if (upstreamSummary) {
        upstreamSummary.textContent = "Upstream: " + (data.upstream_label || "All upstreams") + ".";
      }
      addNew(data.entries);
    };
    request.send();
  }

  window.setInterval(tick, 1000);
})();
