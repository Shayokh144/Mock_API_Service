// Confirm dialog for destructive actions. No inline script, so it works
// under the dashboard CSP. Without JavaScript the trigger still works on
// its own: a form posts directly and a link opens its confirmation page.
//
// Triggers carry data attributes:
//   data-confirm        the question shown in the dialog (required)
//   data-confirm-title  dialog heading
//   data-confirm-label  text of the danger button
//   data-confirm-post   on a link, the URL to POST to after confirming
(function () {
  var dialog = document.getElementById("confirm-dialog");
  if (!dialog || typeof dialog.showModal !== "function") {
    return;
  }
  var title = document.getElementById("confirm-title");
  var message = document.getElementById("confirm-message");
  var ok = document.getElementById("confirm-ok");
  var cancel = document.getElementById("confirm-cancel");
  var pending = null;

  function open(trigger, run) {
    title.textContent = trigger.getAttribute("data-confirm-title") || "Are you sure?";
    message.textContent = trigger.getAttribute("data-confirm") || "";
    ok.textContent = trigger.getAttribute("data-confirm-label") || "Confirm";
    pending = run;
    if (!dialog.open) {
      dialog.showModal();
    }
    cancel.focus();
  }

  function postTo(url) {
    var form = document.createElement("form");
    form.method = "post";
    form.action = url;
    form.hidden = true;
    document.body.appendChild(form);
    form.submit();
  }

  document.addEventListener("submit", function (event) {
    var form = event.target;
    if (!(form instanceof HTMLFormElement) || !form.hasAttribute("data-confirm")) {
      return;
    }
    event.preventDefault();
    open(form, function () {
      // form.submit() skips the submit event, so this does not loop.
      form.submit();
    });
  }, true);

  document.addEventListener("click", function (event) {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }
    var link = event.target instanceof Element ? event.target.closest("a[data-confirm][data-confirm-post]") : null;
    if (!link) {
      return;
    }
    event.preventDefault();
    var url = link.getAttribute("data-confirm-post");
    open(link, function () {
      postTo(url);
    });
  });

  ok.addEventListener("click", function () {
    var run = pending;
    pending = null;
    dialog.close("confirm");
    if (run) {
      run();
    }
  });
  cancel.addEventListener("click", function () {
    pending = null;
    dialog.close("cancel");
  });
  dialog.addEventListener("cancel", function () {
    pending = null;
  });
  dialog.addEventListener("click", function (event) {
    if (event.target === dialog) {
      pending = null;
      dialog.close("cancel");
    }
  });
})();
