function init() {
  document.querySelectorAll(".force-module-button").forEach(function (button) {
    button.addEventListener("click", function () {
      // Disable the buttons and close existing alert
      setForcedButtonDisabled(true)

      if (document.querySelector("#force-alert")) {
        document.querySelector("#force-alert").alert("close")
      }

      forceRun(button.dataset.namespace, button.dataset.name)
    })
  })
}

// Send an XHR request to the server to force a run.
function forceRun(namespace, module) {
  console.log(namespace, module)
  url = window.location.href + "api/v1/forceRun"

  fetch(url, {
    method: "post",
    headers: { "Content-Type": "application/json" },

    body: JSON.stringify({
      namespace: namespace,
      module: module,
    }),
  })
    .then((response) => response.json())
    .then((data) => {
      showForceAlert(true, data.message)

      setForcedButtonDisabled(false)
    })
    .catch((err) => {
      showForceAlert(
        false,
        "Error: " + err + "<br/>See container logs for more info."
      )

      setForcedButtonDisabled(true)
    })
}

function showForceAlert(success, message) {
  type = success ? "success" : "warning"
  const alertPlaceholder = document.getElementById("force-alert-container")
  const wrapper = document.createElement("div")
  wrapper.innerHTML = [
    `<div class="alert alert-${type} alert-dismissible" role="alert">`,
    `   <div>${message}</div>`,
    '   <button type="button" class="btn-close" data-bs-dismiss="alert" aria-label="Close"></button>',
    "</div>",
  ].join("")

  alertPlaceholder.append(wrapper)
}

function setForcedButtonDisabled(disabled) {
  document.querySelectorAll(".force-button").forEach(function (btn) {
    btn.disabled = disabled
  })
}
