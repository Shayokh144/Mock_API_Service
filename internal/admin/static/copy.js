(function () {
  var buttons = document.querySelectorAll("[data-copy]");
  for (var i = 0; i < buttons.length; i++) {
    buttons[i].addEventListener("click", onCopy);
  }

  function onCopy(event) {
    var button = event.currentTarget;
    var node = document.getElementById(button.getAttribute("data-copy"));
    if (!node) {
      return;
    }
    var text = node.textContent || "";
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(function () {
        button.textContent = "Copied";
      }, function () {
        selectNode(node);
      });
      return;
    }
    selectNode(node);
  }

  function selectNode(node) {
    var range = document.createRange();
    range.selectNodeContents(node);
    var selection = window.getSelection();
    if (!selection) {
      return;
    }
    selection.removeAllRanges();
    selection.addRange(range);
  }
})();
