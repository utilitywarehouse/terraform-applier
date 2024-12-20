function init() {
  initialise()

  window.onhashchange = function () {
    initialise()
  }
}

function initialise() {
  const hash = window.location.hash.replace("#", "")

  filterModulesList(hash)

  // if hash contains module name then load module as well
  const values = hash.split("_")
  if (values.length == 2 && values[1] !== "") {
    loadModule(values[0], values[1])
  }
}

function filterModulesList(hash) {

  const namespaceList = document.getElementById("namespace-list");
  const moduleList = document.getElementById("module-list");

  for (const namespace of namespaceList.children) {
    namespace.classList.remove("active");
    // hash can be namespaced name of the module in that case
    // filter all modules of that namespace
    if (hash.startsWith(namespace.dataset.filter)) {
      namespace.classList.add("active");
    }
  }

  for (const modules of moduleList.children) {
    modules.classList.add("d-none");
    modules.classList.remove("d-block");
    modules.classList.remove("active");
    // hash can be namespaced name of the module in that case
    // filter all modules of that namespace
    if (hash.startsWith(modules.dataset.filter) || hash === "") {
      modules.classList.add("d-block");
      modules.classList.remove("d-none");
    }
  }

  if (document.getElementById(hash + "-list")) {
    document.getElementById(hash + "-list").classList.add("active");
  }
}

// Send an XHR request to the server to force a run.
function forceRun(namespace, module, planOnly) {
  // Disable the buttons and close existing alert
  setForcedButtonDisabled(true)

  closeOpenAlert()

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

      // load module after 10sec to update status
      setTimeout(function () { reLoadModule(namespace, module) }, 10000);
    })
    .catch((err) => {
      showForceAlert(
        false,
        "Error: " + err + "<br/>See container logs for more info."
      )
      setForcedButtonDisabled(true)
    })
}

function reLoadModule(namespace, module) {
  // since this function is called recursively after wait its important to check if module 
  // is still loaded.
  if (document.getElementById("module-info").firstElementChild) {
    const values = document.getElementById("module-info").firstElementChild.id.split("_")
    if (values.length != 2 || values[0] !== namespace || values[1] !== module) {
      return
    }
  }

  return loadModule(namespace, module)
}

// Send an XHR request to the server to get module info including run outputs
function loadModule(namespace, module) {
  closeOpenAlert()

  url = window.location.origin + "/module"

  fetch(url, {
    method: "post",
    headers: { "Content-Type": "application/json" },

    body: JSON.stringify({
      namespace: namespace,
      module: module
    }),
  })
    .then(function (data) {
      return data.text();
    })
    .then((html) => {
      // update module template 
      const moduleElm = document.getElementById("module-info")
      moduleElm.innerHTML = html;

      // get current state value to update state in modules list as well
      const state = moduleElm.getElementsByClassName("moduleState")[0].innerText

      // update state in modules list as well
      const listStatusElm = document.getElementById(namespace + "_" + module + "-list").getElementsByClassName("moduleStateList")[0]
      listStatusElm.innerText = state
      listStatusElm.setAttribute("module-state", state);

      Prism.highlightAll();

      if (state === "Running") {
        // re-load module after 10sec to update status
        setTimeout(function () { reLoadModule(namespace, module) }, 10000);
      }
    })
    .catch((err) => {
      showForceAlert(
        false,
        "Error: " + err + "<br/>See container logs for more info."
      )
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

function closeOpenAlert() {
  for (const alert of document.getElementsByClassName("alert")) {
    bootstrap.Alert.getOrCreateInstance(alert).close()
  }
}

function setForcedButtonDisabled(disabled) {
  document.querySelectorAll(".force-button").forEach(function (btn) {
    btn.disabled = disabled
  })
}

