(function () {
  var form = document.getElementById("rule-form");
  if (!form) {
    return;
  }
  var status = document.getElementById("status_code");
  function apply() {
    var selected = form.querySelector('input[name="response_mode"]:checked');
    var raw = selected && selected.value === "raw";
    form.classList.toggle("mode-raw", raw);
    form.classList.toggle("mode-fields", !raw);
    if (status) {
      if (raw) {
        status.removeAttribute("required");
      } else {
        status.setAttribute("required", "");
      }
    }
  }
  var radios = form.querySelectorAll('input[name="response_mode"]');
  for (var i = 0; i < radios.length; i++) {
    radios[i].addEventListener("change", apply);
  }
  apply();
})();
