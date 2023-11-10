function init() {
  filterModules(window.location.hash)
  window.onhashchange = function () {
    filterModules(window.location.hash)
  }

  document.querySelectorAll(".force-module-button").forEach(function (button) {
    button.addEventListener("click", function () {
      // Disable the buttons and close existing alert
      setForcedButtonDisabled(true)

      if (document.querySelector("#force-alert")) {
        document.querySelector("#force-alert").alert("close")
      }

      forceRun(button.dataset.namespace, button.dataset.name, button.dataset.planOnly)
    })
  })
}

function filterModules(hash) {
  hash = hash.replace("#", "")

  const namespaceList = document.getElementById("namespace-list");
  const moduleList = document.getElementById("module-list");
  const moduleInfoList = document.getElementById("module-info-list");

  for (const child of namespaceList.children) {
    child.classList.remove("active");
    // hash can be namespaced name of the module in that case
    // filter all modules of that namespace
    if (hash.startsWith(child.dataset.filter)) {
      child.classList.add("active");
    }
  }

  for (const child of moduleList.children) {
    child.classList.add("d-none");
    child.classList.remove("d-block");
    child.classList.remove("active");
    // hash can be namespaced name of the module in that case
    // filter all modules of that namespace
    if (hash.startsWith(child.dataset.filter) || hash === "") {
      child.classList.add("d-block");
      child.classList.remove("d-none");
    }
  }

  if (document.getElementById(hash + "-list")) {
    document.getElementById(hash + "-list").classList.add("active");
  }

  for (const child of moduleInfoList.children) {
    child.classList.add("d-none");
    child.classList.remove("d-block");

    if (child.id === hash) {
      child.classList.add("d-block");
      child.classList.remove("d-none");
    }
  }
}

// Send an XHR request to the server to force a run.
function forceRun(namespace, module, planOnly) {
  console.log(namespace, module, planOnly)
  url = window.location.origin + "/api/v1/forceRun"

  fetch(url, {
    method: "post",
    headers: { "Content-Type": "application/json" },

    body: JSON.stringify({
      namespace: namespace,
      module: module,
      planOnly: planOnly,
    }),
  })
    .then((response) => response.json())
    .then((data) => {
      if (data.result == "success") {
        showForceAlert(true, data.message)
      } else {
        showForceAlert(false, data.message)
      }

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
  type = success ? "success" : "danger"
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
