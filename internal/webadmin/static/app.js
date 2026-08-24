// HomePi Web Admin client. Vanilla JS, no frameworks, no external
// resources. All state-changing requests wait for the same-origin CSRF
// bootstrap and all dynamic content is rendered with textContent.
(function () {
  "use strict";

  var csrfToken = "";
  var csrfReady = fetch("/api/bootstrap", {
    method: "GET",
    credentials: "same-origin",
    headers: { "Accept": "application/json" }
  }).then(function (response) {
    if (!response.ok) throw new Error("CSRF bootstrap failed: " + response.status);
    return response.json();
  }).then(function (payload) {
    csrfToken = payload.csrf_token || "";
    if (!csrfToken) throw new Error("CSRF bootstrap returned no token");
  }).catch(function (error) {
    showGlobalFeedback(error.message, "error");
    throw error;
  });

  var TYPE_INFO = [
    { type: "mock", label: "Mock Fixture", secret: false, auth: false, mock: true, regions: ["global"] },
    { type: "codex_usage", label: "Codex (wham/usage)", secret: false, auth: true, regions: ["global"] },
    { type: "grok_usage", label: "Grok Usage", secret: false, auth: true, authLabel: "Grok CLI auth.json", regions: ["global", "custom"] },
    { type: "deepseek_api", label: "DeepSeek API", secret: true, auth: false, regions: ["global", "cn"] },
    { type: "kimi_api", label: "Kimi (Moonshot) API", secret: true, auth: false, regions: ["global", "cn"] },
    { type: "kimi_coding", label: "Kimi Coding Plan", secret: true, auth: false, regions: ["global", "cn"] },
    { type: "minimax_coding", label: "MiniMax Coding Plan", secret: true, auth: false, regions: ["global", "cn"] },
    { type: "prometheus", label: "Prometheus", secret: false, auth: false, options: true, regions: ["custom"] },
    { type: "grafana", label: "Grafana", secret: true, auth: false, options: true, regions: ["custom"] },
    { type: "portainer", label: "Portainer", secret: true, auth: false, options: true, regions: ["custom"] }
  ];

  var providerForm = document.getElementById("provider-form");
  var typeSelect = document.getElementById("type-select");
  var regionSelect = document.getElementById("region-select");
  var saveButton = document.getElementById("save-btn");
  var testButton = document.getElementById("test-btn");
  var applyButton = document.getElementById("apply-btn");
  var cancelEditButton = document.getElementById("cancel-edit-btn");
  var editingProviderID = "";
  var displayForm = document.getElementById("display-form");
  var displayTestButton = document.getElementById("display-test-btn");
  var displayApplyButton = document.getElementById("display-apply-btn");
  var displayTested = false;
  var kioskForm = document.getElementById("kiosk-control-form");
  var kioskAction = document.getElementById("kiosk-action");

  TYPE_INFO.forEach(function (info) {
    var option = document.createElement("option");
    option.value = info.type;
    option.textContent = info.label;
    typeSelect.appendChild(option);
  });
  typeSelect.addEventListener("change", updateTypeVisibility);
  regionSelect.addEventListener("change", updateBaseURLVisibility);
  updateTypeVisibility();

  displayForm.addEventListener("submit", function (event) {
    event.preventDefault();
    var values = new FormData(displayForm);
    setDisplayBusy(true);
    api("PUT", "/api/display/profile", {
      ssh_host: String(values.get("ssh_host") || ""),
      device_id: String(values.get("device_id") || ""),
      node_url: String(values.get("node_url") || ""),
      style: String(values.get("style") || "rich"),
      data_dir: String(values.get("data_dir") || ""),
      page_order: String(values.get("page_order") || ""),
      page_dwell_seconds: String(values.get("page_dwell_seconds") || "")
    }).then(function (state) {
      renderDisplayProfile(state);
      showDisplayFeedback("Display draft saved in memory. Test SSH before applying it.", "success");
    }).catch(function (error) {
      showDisplayFeedback(humanError(error), "error");
    }).finally(function () { setDisplayBusy(false); });
  });

  displayTestButton.addEventListener("click", function () {
    setDisplayBusy(true);
    showDisplayFeedback("Validating the profile, SSH connection, remote service, and candidate environment…", "info");
    api("POST", "/api/display/test", {}).then(function (result) {
      showDisplayFeedback("SSH test passed. Candidate validated on " + (result.remote.binary_version || "the display") + ".", "success");
      displayTested = true;
    }).catch(function (error) {
      showDisplayFeedback(humanError(error), "error");
    }).finally(function () { setDisplayBusy(false); });
  });

  displayApplyButton.addEventListener("click", function () {
    setDisplayBusy(true);
    showDisplayFeedback("Applying the fixed environment transaction and waiting for kiosk health…", "info");
    api("POST", "/api/display/apply", {}).then(function (result) {
      showDisplayFeedback("Display configuration applied. Service and snapshot health are confirmed.", "success");
      displayTested = false;
      renderRemoteDisplay(result.remote || {});
      loadDisplay();
    }).catch(function (error) {
      var rolledBack = error.payload && error.payload.rolled_back;
      var prefix = rolledBack ? "Apply failed; the previous display environment was restored. " : "";
      showDisplayFeedback(prefix + humanError(error), "error");
      if (rolledBack) displayTested = false;
    }).finally(function () { setDisplayBusy(false); });
  });

  kioskAction.addEventListener("change", updateKioskFields);
  updateKioskFields();
  kioskForm.addEventListener("submit", function (event) {
    event.preventDefault();
    var action = kioskAction.value;
    var body = { action: action };
    var duration = Number(document.getElementById("kiosk-duration").value || 0);
    if (action === "show_page") body.page_id = document.getElementById("kiosk-page").value;
    if (["show_page", "next_page", "previous_page", "set_rotation", "show_message"].indexOf(action) >= 0) body.duration_seconds = duration;
    if (action === "set_rotation") {
      body.enabled = document.getElementById("kiosk-rotation-enabled").value === "true";
      body.interval_seconds = Number(document.getElementById("kiosk-interval").value || 0);
    }
    if (action === "refresh_data") {
      body.connector_ids = Array.prototype.slice.call(document.querySelectorAll("#kiosk-connectors input:checked")).map(function (input) { return input.value; });
    }
    if (action === "show_message") {
      body.text = document.getElementById("kiosk-message").value;
      body.severity = document.getElementById("kiosk-severity").value;
    }
    if (action === "set_brightness") body.level = Number(document.getElementById("kiosk-level").value || 0);
    setKioskBusy(true);
    showKioskFeedback("Publishing command…", "info");
    api("POST", "/api/display/control", body).then(function (result) {
      renderKioskResult(result);
      showKioskFeedback("Command " + result.command_id + " published. Waiting for the Kiosk…", "success");
      return pollKioskCommand(result.command_id);
    }).catch(function (error) {
      showKioskFeedback(humanError(error), "error");
    }).finally(function () { setKioskBusy(false); });
  });

  document.querySelectorAll("[data-tab]").forEach(function (link) {
    link.addEventListener("click", function (event) {
      event.preventDefault();
      showTab(link.dataset.tab, true);
    });
  });

  window.addEventListener("hashchange", function () {
    var name = window.location.hash.slice(1);
    if (isKnownTab(name)) showTab(name, false);
  });

  function isKnownTab(name) {
    return name === "overview" || name === "providers" || name === "display";
  }

  function showTab(name, updateHash) {
    if (!isKnownTab(name)) name = "overview";
    document.querySelectorAll(".tab").forEach(function (section) {
      section.hidden = section.id !== name;
    });
    document.querySelectorAll("nav [data-tab]").forEach(function (link) {
      var active = link.dataset.tab === name;
      link.classList.toggle("active", active);
      if (active) link.setAttribute("aria-current", "page");
      else link.removeAttribute("aria-current");
    });
    if (updateHash && window.location.hash !== "#" + name) {
      window.history.replaceState(null, "", "#" + name);
    }
    document.title = titleForTab(name) + " · HomePi Node Admin";
    if (name === "overview") loadStatus();
    if (name === "providers") {
      loadProviders();
      loadRuntimeStatus();
    }
    if (name === "display") loadDisplay();
  }

  function titleForTab(name) {
    if (name === "providers") return "Providers";
    if (name === "display") return "Display";
    return "Overview";
  }

  function api(method, path, body) {
    function send() {
      var options = {
        method: method,
        credentials: "same-origin",
        headers: { "Accept": "application/json", "Content-Type": "application/json" }
      };
      if (method !== "GET" && method !== "HEAD") {
        options.headers["X-CSRF-Token"] = csrfToken;
      }
      if (body !== undefined) options.body = JSON.stringify(body);
      return fetch(path, options).then(function (response) {
        return response.text().then(function (text) {
          var payload = {};
          try { payload = text ? JSON.parse(text) : {}; }
          catch (_) { payload = { error: text || "Unexpected response" }; }
          if (!response.ok) {
            var error = new Error(payload.error || ("Request failed with status " + response.status));
            error.status = response.status;
            error.payload = payload;
            throw error;
          }
          return payload;
        });
      });
    }
    if (method === "GET" || method === "HEAD") return send();
    return csrfReady.then(send);
  }

  function loadStatus() {
    return api("GET", "/api/status").then(function (status) {
      renderNode(status.node);
      renderRuntime(status.runtime || {});
      renderOverviewProviders(status.providers || []);
      renderDisplaySummary(status.display || {});
      renderLastApply(status.last_apply || {});
    }).catch(function (error) {
      showGlobalFeedback("Unable to load node status: " + error.message, "error");
    });
  }

  function loadRuntimeStatus() {
    return api("GET", "/api/status").then(function (status) {
      renderRuntime(status.runtime || {});
      return status.runtime || {};
    }).catch(function () {
      renderRuntime({
        state: "unavailable",
        message: "The installed service version could not be checked."
      });
    });
  }

  function renderRuntime(runtime) {
    runtime = runtime || {};
    var state = runtime.state || "unavailable";
    var signal = document.getElementById("runtime-signal");
    var mark = document.getElementById("runtime-mark");
    var title = document.getElementById("runtime-title");
    var message = document.getElementById("runtime-message");
    var runtimeBadge = document.getElementById("runtime-badge");
    var applyHint = document.getElementById("apply-runtime-hint");
    var adminBuild = buildLabel(runtime.admin);
    var serviceBuild = buildLabel(runtime.service);
    var labels = {
      synced: ["Runtime versions aligned", "Synced", "ok", "✓"],
      version_mismatch: ["Service update required", "Version drift", "warn", "!"],
      different_binary: ["Different launch source", "Check source", "warn", "↗"],
      service_stopped: ["Service is stopped", "Stopped", "warn", "!"],
      not_installed: ["Service is not installed", "Not installed", "warn", "!"],
      unavailable: ["Version check unavailable", "Unavailable", "muted", "?"]
    };
    var view = labels[state] || labels.unavailable;

    signal.className = "runtime-signal " + state;
    mark.textContent = view[3];
    title.textContent = view[0];
    message.textContent = runtime.message || "The installed service version could not be checked.";
    setBadge(runtimeBadge, view[1], view[2]);
    document.getElementById("runtime-admin-build").textContent = adminBuild;
    document.getElementById("runtime-service-build").textContent = serviceBuild;

    applyHint.className = "apply-runtime-hint " + (state === "synced" ? "ok" : state === "unavailable" ? "neutral" : "warn");
    if (state === "synced") {
      applyHint.textContent = "Apply will restart Service " + serviceBuild + ", aligned with this Admin.";
    } else if (state === "version_mismatch" || state === "different_binary") {
      applyHint.textContent = "Apply will restart Service " + serviceBuild + ", not Admin " + adminBuild + ". Install or reopen the aligned build first.";
    } else if (state === "service_stopped") {
      applyHint.textContent = "Apply targets Service " + serviceBuild + ", which is currently stopped.";
    } else if (state === "not_installed") {
      applyHint.textContent = "Apply cannot restart HomePi until the user service is installed.";
    } else {
      applyHint.textContent = "Apply target version is unavailable. Provider draft editing and read-only tests remain available.";
    }
  }

  function buildLabel(build) {
    build = build || {};
    if (!build.version) return "Unavailable";
    return build.version + (build.commit ? " · " + build.commit : "");
  }

  function renderNode(node) {
    node = node || {};
    document.getElementById("node-name").textContent = node.label || node.id || "Unnamed node";
    document.getElementById("node-address").textContent = node.listen_addr || "Listener unavailable";
    document.getElementById("provider-count").textContent = String(node.provider_count || 0);
    document.getElementById("revision").textContent = node.revision ? shortRevision(node.revision) : "no revision";
    renderInfoList(document.getElementById("node-info"), [
      ["Node ID", node.id || "—"],
      ["Label", node.label || "—"],
      ["Listen address", node.listen_addr || "—"],
      ["Secret backend", node.secret_backend || "—"],
      ["Providers", String(node.provider_count || 0)],
      ["Devices", String(node.device_count || 0)],
      ["Revision", node.revision || "—"]
    ]);
  }

  function renderOverviewProviders(providers) {
    var tbody = document.querySelector("#overview-providers tbody");
    var tableWrap = document.querySelector("#overview-providers").closest(".table-wrap");
    var empty = document.getElementById("overview-empty");
    tbody.replaceChildren();
    tableWrap.hidden = providers.length === 0;
    empty.hidden = providers.length !== 0;

    var pending = 0;
    providers.forEach(function (provider) {
      if (provider.pending) pending += 1;
      var row = document.createElement("tr");
      row.appendChild(providerNameCell(provider));
      row.appendChild(td(typeLabel(provider.type)));
      row.appendChild(td(provider.region || "—"));
      row.appendChild(td(provider.interval || "—"));
      row.appendChild(td(provider.secret_present ? badge("Configured", "ok") : badge("Not set", "muted")));
      row.appendChild(td(provider.pending ? badge("Pending", "warn") : badge(provider.enabled === false ? "Disabled" : "Ready", provider.enabled === false ? "muted" : "ok")));
      tbody.appendChild(row);
    });

    var pendingBadge = document.getElementById("pending-badge");
    var providerSummary = document.getElementById("provider-summary");
    if (pending > 0) {
      setBadge(pendingBadge, pending + " pending", "warn");
      providerSummary.textContent = pending + " change" + (pending === 1 ? "" : "s") + " awaiting Apply";
    } else {
      setBadge(pendingBadge, "No draft", "muted");
      providerSummary.textContent = providers.length === 0 ? "No data sources configured" : "Configuration is in sync";
    }
  }

  function renderDisplaySummary(display) {
    var connected = Boolean(display.connected);
    var state = connected ? "Connected" : "Offline";
    var detail = connected ? (display.theme ? "Theme · " + display.theme : "Snapshot stream active") : (display.note || "No active display connection");
    document.getElementById("display-state").textContent = state;
    document.getElementById("display-summary").textContent = detail;
    setBadge(document.getElementById("display-badge"), state, connected ? "ok" : "muted");
  }

  function renderLastApply(lastApply) {
    var target = document.getElementById("last-apply");
    target.replaceChildren();
    if (!lastApply.status) {
      target.className = "empty-state compact";
      target.appendChild(element("span", "empty-glyph", "✓"));
      target.appendChild(element("strong", "", "No apply recorded"));
      target.appendChild(element("span", "", "Your current configuration is the baseline."));
      return;
    }
    target.className = "result-content";
    var ok = lastApply.status === "completed" || lastApply.status === "ok" || lastApply.status === "success";
    var summary = element("div", "result-summary");
    summary.appendChild(element("span", "result-mark" + (ok ? "" : " error"), ok ? "✓" : "!"));
    summary.appendChild(element("strong", "", ok ? "Configuration applied" : "Apply needs attention"));
    summary.appendChild(element("span", "", lastApply.note || lastApply.status));
    target.appendChild(summary);
    if (lastApply.at) {
      var meta = element("div", "result-meta");
      meta.appendChild(element("span", "", formatTimestamp(lastApply.at)));
      meta.appendChild(element("span", "", lastApply.status));
      target.appendChild(meta);
    }
  }

  function loadProviders() {
    return api("GET", "/api/draft").then(function (draft) {
      renderProviderDraft(draft);
      return draft;
    }).catch(function (error) {
      showProviderFeedback("Unable to load provider draft: " + error.message, "error");
    });
  }

  function renderProviderDraft(draft) {
    draft = draft || {};
    var providers = draft.providers || [];
    var tbody = document.querySelector("#provider-table tbody");
    var tableWrap = document.querySelector("#provider-table").closest(".table-wrap");
    var empty = document.getElementById("provider-empty");
    tbody.replaceChildren();
    providers.forEach(function (provider) {
      var row = document.createElement("tr");
      row.appendChild(providerNameCell(provider));
      row.appendChild(td(typeLabel(provider.type)));
      row.appendChild(td(provider.region || "—"));
      row.appendChild(td(provider.enabled === false ? badge("Disabled", "muted") : badge("Enabled", "ok")));
      row.appendChild(td(provider.secret_ref ? badge("Set", "ok") : badge("None", "muted")));
      var action = document.createElement("td");
      var actions = element("div", "table-actions");
      var editButton = element("button", "quiet", "Edit");
      editButton.type = "button";
      editButton.setAttribute("aria-label", "Edit provider " + provider.id);
      editButton.addEventListener("click", function () { beginProviderEdit(provider); });
      actions.appendChild(editButton);
      var toggleButton = element("button", "toggle", provider.enabled === false ? "Enable" : "Disable");
      toggleButton.type = "button";
      toggleButton.setAttribute("aria-label", (provider.enabled === false ? "Enable" : "Disable") + " provider " + provider.id);
      toggleButton.addEventListener("click", function () { toggleProvider(provider, toggleButton); });
      actions.appendChild(toggleButton);
      var deleteButton = element("button", "danger", "Delete");
      deleteButton.type = "button";
      deleteButton.setAttribute("aria-label", "Delete provider " + provider.id);
      deleteButton.addEventListener("click", function () { deleteProvider(provider.id, deleteButton); });
      actions.appendChild(deleteButton);
      action.appendChild(actions);
      row.appendChild(action);
      tbody.appendChild(row);
    });
    tableWrap.hidden = providers.length === 0;
    empty.hidden = providers.length !== 0;
    document.getElementById("configured-count").textContent = providers.length + " provider" + (providers.length === 1 ? "" : "s");
    renderDraftDiff(draft.diff || [], Boolean(draft.has_pending));
  }

  function renderDraftDiff(diff, hasPending) {
    var card = document.getElementById("draft-card");
    var list = document.getElementById("draft-diff");
    var state = document.getElementById("draft-state");
    list.replaceChildren();
    card.hidden = !hasPending;
    applyButton.disabled = !hasPending;
    if (!hasPending) {
      state.className = "secure-chip neutral";
      state.replaceChildren(element("span", "", "◆"), document.createTextNode(" No pending changes"));
      return;
    }
    state.className = "secure-chip warn";
    state.replaceChildren(element("span", "", "◆"), document.createTextNode(" " + diff.length + " pending change" + (diff.length === 1 ? "" : "s")));
    diff.forEach(function (change) {
      var operation = change.Op || change.op || "modified";
      var item = element("div", "change-item");
      item.appendChild(badge(operation, operation === "removed" ? "danger" : operation === "added" ? "ok" : "warn"));
      item.appendChild(element("strong", "", change.ID || change.id || "Provider"));
      var fields = change.Fields || change.fields || [];
      var detail = change.Type || change.type || "provider";
      if (fields.length) detail += " · " + fields.join(", ");
      item.appendChild(element("small", "", detail));
      list.appendChild(item);
    });
  }

  providerForm.addEventListener("submit", function (event) {
    event.preventDefault();
    if (!providerForm.reportValidity()) return;
    var data = new FormData(providerForm);
    var info = TYPE_INFO.find(function (item) { return item.type === typeSelect.value; }) || {};
    var options;
    if (info.options) {
      var rawOptions = String(data.get("options_json") || "").trim();
      try {
        options = rawOptions ? JSON.parse(rawOptions) : {};
      } catch (_) {
        showProviderFeedback("HomeLab options must be a JSON object of string values.", "error");
        return;
      }
      if (!options || Array.isArray(options) || typeof options !== "object" || Object.keys(options).some(function (key) { return typeof options[key] !== "string"; })) {
        showProviderFeedback("HomeLab options must be a JSON object of string values.", "error");
        return;
      }
    }
    var body = {
      id: data.get("id"),
      new_type: typeSelect.value,
      account_label: data.get("account_label"),
      region: data.get("region"),
      base_url: data.get("base_url") || undefined,
      interval: data.get("interval"),
      stale_after: data.get("stale_after"),
      enabled: data.get("enabled") === "on",
      auth_file: data.get("auth_file") || undefined,
      mock_fixture: data.get("mock_fixture") || undefined,
      options: options,
      candidate_secret: data.get("candidate_secret") || undefined
    };
    setBusy(saveButton, true);
    showProviderFeedback("Validating and saving the draft…", "info");
    api("POST", "/api/draft", body).then(function (draft) {
      providerForm.querySelector('input[name="candidate_secret"]').value = "";
      renderProviderDraft(draft);
      if (editingProviderID) endProviderEdit();
      document.getElementById("test-card").hidden = true;
      showProviderFeedback("Draft saved. Test the provider before applying enabled changes.", "success");
    }).catch(function (error) {
      showProviderFeedback(humanError(error), "error");
    }).finally(function () {
      setBusy(saveButton, false);
    });
  });

  cancelEditButton.addEventListener("click", function () {
    var id = editingProviderID;
    endProviderEdit();
    showProviderFeedback("Editing " + id + " cancelled. No draft changes were made.", "info");
  });

  testButton.addEventListener("click", function () {
    var data = new FormData(providerForm);
    var id = data.get("id");
    if (!id) {
      showProviderFeedback("Enter a Provider ID before running a test.", "error");
      providerForm.elements.id.focus();
      return;
    }
    var card = document.getElementById("test-card");
    var output = document.getElementById("test-output");
    card.hidden = false;
    output.replaceChildren(renderLoading("Testing " + id + " without changing your configuration…"));
    setBusy(testButton, true);
    api("POST", "/api/draft/test", { id: id }).then(function (result) {
      renderTestResult(output, result);
      if (result.class) showProviderFeedback("Provider test completed with a classified error.", "error");
      else showProviderFeedback("Provider test passed. The current draft is ready for review.", "success");
    }).catch(function (error) {
      renderFailure(output, "Test could not run", humanError(error));
      showProviderFeedback(humanError(error), "error");
    }).finally(function () {
      setBusy(testButton, false);
    });
  });

  applyButton.addEventListener("click", function () {
    var card = document.getElementById("apply-card");
    var output = document.getElementById("apply-output");
    card.hidden = false;
    output.replaceChildren(renderLoading("Applying configuration and checking node health…"));
    setBusy(applyButton, true);
    setBusy(saveButton, true);
    setBusy(testButton, true);
    api("POST", "/api/draft/apply", {}).then(function (result) {
      renderApplyResult(output, result, true);
      showProviderFeedback("Configuration applied and health checks passed.", "success");
      return loadProviders().then(loadStatus);
    }).catch(function (error) {
      renderApplyResult(output, error.payload || { error: error.message }, false);
      showProviderFeedback(humanError(error), "error");
    }).finally(function () {
      setBusy(saveButton, false);
      setBusy(testButton, false);
      applyButton.removeAttribute("aria-busy");
      loadProviders();
    });
  });

  function deleteProvider(id, button) {
    if (!window.confirm("Delete provider " + id + " from the draft?")) return;
    setBusy(button, true);
    api("DELETE", "/api/draft", { id: id }).then(function (draft) {
      renderProviderDraft(draft);
      if (editingProviderID === id) endProviderEdit();
      showProviderFeedback("Provider " + id + " removed from the draft.", "success");
    }).catch(function (error) {
      showProviderFeedback(humanError(error), "error");
      setBusy(button, false);
    });
  }

  function beginProviderEdit(provider) {
    editingProviderID = provider.id;
    providerForm.reset();
    providerForm.elements.id.value = provider.id || "";
    providerForm.elements.id.readOnly = true;
    typeSelect.value = provider.type || "";
    typeSelect.disabled = true;
    updateTypeVisibility();
    regionSelect.value = provider.region || "global";
    updateBaseURLVisibility();
    providerForm.elements.account_label.value = provider.account_label || "";
    providerForm.elements.base_url.value = provider.base_url || "";
    providerForm.elements.interval.value = provider.interval || "60s";
    providerForm.elements.stale_after.value = provider.stale_after || "5m";
    providerForm.elements.auth_file.value = provider.auth_file || "";
    providerForm.elements.mock_fixture.value = provider.mock_fixture || "";
    providerForm.elements.options_json.value = provider.options ? JSON.stringify(provider.options, null, 2) : "";
    providerForm.elements.candidate_secret.value = "";
    providerForm.elements.enabled.checked = provider.enabled !== false;
    document.getElementById("editor-mode").textContent = "Editing · " + provider.id;
    document.getElementById("editor-title").textContent = "Edit provider";
    cancelEditButton.hidden = false;
    saveButton.replaceChildren(element("span", "", "✓"), document.createTextNode(" Save changes"));
    providerForm.querySelector(".workflow-actions").classList.add("editing");
    showProviderFeedback("Editing " + provider.id + ". Leave the secret blank to keep the existing credential.", "info");
    providerForm.elements.account_label.focus();
  }

  function endProviderEdit() {
    editingProviderID = "";
    providerForm.reset();
    providerForm.elements.id.readOnly = false;
    typeSelect.disabled = false;
    updateTypeVisibility();
    document.getElementById("editor-mode").textContent = "Draft editor";
    document.getElementById("editor-title").textContent = "Add provider";
    cancelEditButton.hidden = true;
    saveButton.replaceChildren(element("span", "", "＋"), document.createTextNode(" Save draft"));
    providerForm.querySelector(".workflow-actions").classList.remove("editing");
  }

  function toggleProvider(provider, button) {
    var enabled = provider.enabled === false;
    setBusy(button, true);
    api("PUT", "/api/draft", { id: provider.id, enabled: enabled }).then(function (draft) {
      renderProviderDraft(draft);
      if (editingProviderID === provider.id) providerForm.elements.enabled.checked = enabled;
      var verb = enabled ? "enabled" : "disabled";
      var next = enabled ? " Test it before applying the change." : " Apply the draft when you are ready.";
      showProviderFeedback("Provider " + provider.id + " " + verb + " in the draft." + next, "success");
    }).catch(function (error) {
      showProviderFeedback(humanError(error), "error");
      setBusy(button, false);
    });
  }

  function loadDisplay() {
    Promise.all([api("GET", "/api/status"), api("GET", "/api/display/profile")]).then(function (responses) {
      var status = responses[0];
      renderNode(status.node || {});
      renderRuntime(status.runtime || {});
      var display = status.display || {};
      var connected = Boolean(display.connected);
      var heading = connected ? "Display connected" : "Display currently offline";
      var note = display.note || (connected ? "The snapshot stream is active." : "No active display connection was reported.");
      document.getElementById("display-heading").textContent = heading;
      document.getElementById("display-note").textContent = note;
      var pageBadge = document.getElementById("display-page-badge");
      pageBadge.className = "secure-chip " + (connected ? "" : "neutral");
      pageBadge.replaceChildren(element("span", "", "●"), document.createTextNode(" " + (connected ? "Connected" : "Offline")));
      renderInfoList(document.getElementById("display-info"), [
        ["Connected", connected ? "Yes" : "No"],
        ["Theme", display.theme || "—"],
        ["Latest snapshot", display.latest_snapshot ? formatTimestamp(display.latest_snapshot) : "—"],
        ["Source epoch", display.source_epoch || "—"],
        ["Snapshot version", display.snapshot_version ? String(display.snapshot_version) : "—"],
        ["Note", note]
      ]);
      renderDisplayProfile(responses[1]);
      renderKioskControl(status, responses[1]);
    }).catch(function (error) {
      showGlobalFeedback("Unable to load display status: " + error.message, "error");
    });
  }

  function renderKioskControl(status, profileState) {
    var profile = profileState.profile || {};
    var target = profile.device_id || (status.display || {}).device_id || "Not configured";
    document.getElementById("kiosk-target").value = target;
    var available = target !== "Not configured" && Boolean(profileState.configured);
    var badge = document.getElementById("kiosk-control-badge");
    setBadge(badge, available ? "Ready" : "Unavailable", available ? "ok" : "muted");
    document.getElementById("kiosk-submit-btn").disabled = !available;
    var connectors = document.getElementById("kiosk-connectors");
    connectors.replaceChildren();
    (status.providers || []).filter(function (provider) { return provider.enabled && provider.configured; }).forEach(function (provider) {
      var label = document.createElement("label");
      label.className = "checkbox-label";
      var input = document.createElement("input"); input.type = "checkbox"; input.value = provider.id;
      label.appendChild(input); label.appendChild(document.createTextNode(provider.id)); connectors.appendChild(label);
    });
    updateKioskFields();
  }

  function updateKioskFields() {
    var action = kioskAction.value;
    document.getElementById("kiosk-page-row").hidden = action !== "show_page";
    document.getElementById("kiosk-duration-row").hidden = ["show_page", "next_page", "previous_page", "set_rotation", "show_message"].indexOf(action) < 0;
    document.getElementById("kiosk-rotation-enabled-row").hidden = action !== "set_rotation";
    document.getElementById("kiosk-interval-row").hidden = action !== "set_rotation";
    document.getElementById("kiosk-severity-row").hidden = action !== "show_message";
    document.getElementById("kiosk-message-row").hidden = action !== "show_message";
    document.getElementById("kiosk-level-row").hidden = action !== "set_brightness";
    document.getElementById("kiosk-connectors-row").hidden = action !== "refresh_data";
  }

  function setKioskBusy(busy) { setBusy(document.getElementById("kiosk-submit-btn"), busy); }

  function showKioskFeedback(message, kind) {
    var target = document.getElementById("kiosk-control-feedback"); target.hidden = false; target.className = "inline-feedback " + (kind || "info"); target.textContent = message;
  }

  function renderKioskResult(result) {
    var target = document.getElementById("kiosk-control-result"); target.hidden = false; target.replaceChildren();
    var title = result.status ? result.status : "published";
    target.appendChild(element("strong", "", "Kiosk command · " + title));
    target.appendChild(element("span", "", "ID " + (result.command_id || "—") + " · sequence " + (result.sequence || "—")));
    if (result.code) target.appendChild(element("span", "", "code " + result.code));
  }

  function pollKioskCommand(commandID) {
    var attempts = 0;
    return new Promise(function (resolve) {
      function tick() {
        attempts += 1;
        api("GET", "/api/display/control/" + encodeURIComponent(commandID)).then(function (result) {
          renderKioskResult(result);
          if (result.status && ["executed", "rejected", "expired", "failed"].indexOf(result.status) >= 0) {
            showKioskFeedback(result.status === "executed" ? "Kiosk command executed." : "Kiosk command ended: " + result.status + (result.code ? " (" + result.code + ")" : ""), result.status === "executed" ? "success" : "error"); resolve(result); return;
          }
          if (attempts >= 40) { showKioskFeedback("Command is still pending; reopen Display to check its status.", "info"); resolve(result); return; }
          window.setTimeout(tick, 250);
        }).catch(function (error) { showKioskFeedback(humanError(error), "error"); resolve(); });
      }
      tick();
    });
  }

  function renderDisplayProfile(state) {
    var profile = state.profile || {};
    ["ssh_host", "device_id", "node_url", "style", "data_dir", "page_order", "page_dwell_seconds"].forEach(function (name) {
      if (profile[name] !== undefined) displayForm.elements[name].value = profile[name];
    });
    var badge = document.getElementById("display-config-badge");
    var differences = state.differences || [];
    setBadge(badge, differences.length ? "Pending" : (state.configured ? "Saved" : "New"), differences.length ? "warn" : "ok");
    document.getElementById("display-diff").textContent = differences.length ? ("Pending changes: " + differences.join(", ")) : "Desired profile matches the reported display style.";
    displayTested = Boolean(state.tested);
    displayApplyButton.disabled = !displayTested;
    if (!state.token_present) showDisplayFeedback("The paired device token is unavailable in the credential store.", "error");
  }

  function renderRemoteDisplay(remote) {
    if (!remote) return;
    document.getElementById("display-heading").textContent = remote.connected ? "Display connected" : "Display currently offline";
  }

  function setDisplayBusy(busy) {
    setBusy(document.getElementById("display-save-btn"), busy);
    setBusy(displayTestButton, busy);
    setBusy(displayApplyButton, busy || !displayTested);
  }

  function showDisplayFeedback(message, kind) {
    var target = document.getElementById("display-feedback");
    target.hidden = false;
    target.className = "inline-feedback " + (kind || "info");
    target.textContent = message;
  }

  function updateTypeVisibility() {
    var info = TYPE_INFO.find(function (item) { return item.type === typeSelect.value; }) || {};
    document.getElementById("secret-row").hidden = !info.secret;
    document.getElementById("auth-file-row").hidden = !info.auth;
    document.querySelector("#auth-file-row span").textContent = info.authLabel || "Codex auth file";
    document.getElementById("mock-row").hidden = !info.mock;
    document.getElementById("options-row").hidden = !info.options;
    regionSelect.replaceChildren();
    (info.regions || ["global"]).forEach(addRegionOption);
    if (!info.noCustom && (info.regions || []).indexOf("custom") < 0) addRegionOption("custom");
    updateBaseURLVisibility();
  }

  function addRegionOption(region) {
    var option = document.createElement("option");
    option.value = region;
    option.textContent = region;
    regionSelect.appendChild(option);
  }

  function updateBaseURLVisibility() {
    document.getElementById("base-url-row").hidden = regionSelect.value !== "custom";
  }

  function renderTestResult(target, result) {
    target.replaceChildren();
    var passed = !result.class;
    var summary = element("div", "result-summary");
    summary.appendChild(element("span", "result-mark" + (passed ? "" : " error"), passed ? "✓" : "!"));
    summary.appendChild(element("strong", "", passed ? "Provider responded successfully" : "Provider needs attention"));
    summary.appendChild(element("span", "", passed ? (result.metric_count + " metrics returned") : (result.message || result.class || "The test failed")));
    target.appendChild(summary);
    var meta = element("div", "result-meta");
    meta.appendChild(element("span", "", result.provider_id || "unknown provider"));
    meta.appendChild(element("span", "", typeLabel(result.provider_type)));
    meta.appendChild(element("span", "", String(result.elapsed_ms || 0) + " ms"));
    if (result.class) meta.appendChild(element("span", "", result.class));
    target.appendChild(meta);
  }

  function renderApplyResult(target, result, passed) {
    target.replaceChildren();
    var summary = element("div", "result-summary");
    summary.appendChild(element("span", "result-mark" + (passed ? "" : " error"), passed ? "✓" : "!"));
    summary.appendChild(element("strong", "", passed ? "Configuration is live" : "Apply was not completed"));
    var successMessage = result.display_sync === "pending" ? "The node is live; the display will sync when it reconnects." : "The node restarted and passed its health check.";
    summary.appendChild(element("span", "", passed ? successMessage : (result.error || "The transaction was rejected.")));
    target.appendChild(summary);
    var meta = element("div", "result-meta");
    if (result.revision) meta.appendChild(element("span", "", "rev " + shortRevision(result.revision)));
    if (result.persisted_at) meta.appendChild(element("span", "", formatTimestamp(result.persisted_at)));
    if (result.error_step) meta.appendChild(element("span", "", "failed at " + result.error_step));
    if (result.display_sync) meta.appendChild(element("span", "", "display " + result.display_sync));
    if (meta.childNodes.length) target.appendChild(meta);
    if (result.step_log && result.step_log.length) {
      var steps = element("ol", "step-list");
      result.step_log.forEach(function (step) {
        steps.appendChild(element("li", "", (step.Step || step.step || "step") + " · " + (step.Detail || step.detail || step.Status || step.status || "complete")));
      });
      target.appendChild(steps);
    }
  }

  function renderLoading(message) {
    var summary = element("div", "result-summary");
    summary.appendChild(element("span", "result-mark warn", "…"));
    summary.appendChild(element("strong", "", "Working"));
    summary.appendChild(element("span", "", message));
    return summary;
  }

  function renderFailure(target, title, message) {
    target.replaceChildren();
    var summary = element("div", "result-summary");
    summary.appendChild(element("span", "result-mark error", "!"));
    summary.appendChild(element("strong", "", title));
    summary.appendChild(element("span", "", message));
    target.appendChild(summary);
  }

  function renderInfoList(target, rows) {
    target.replaceChildren();
    rows.forEach(function (row) {
      var item = document.createElement("div");
      item.appendChild(element("dt", "", row[0]));
      item.appendChild(element("dd", "", row[1]));
      target.appendChild(item);
    });
  }

  function providerNameCell(provider) {
    var cell = document.createElement("td");
    var wrap = element("div", "provider-name");
    wrap.appendChild(element("strong", "", provider.label || provider.account_label || provider.id || "Unnamed"));
    if ((provider.label || provider.account_label) && provider.id) wrap.appendChild(element("small", "", provider.id));
    cell.appendChild(wrap);
    return cell;
  }

  function td(content) {
    var cell = document.createElement("td");
    if (content instanceof Node) cell.appendChild(content);
    else cell.textContent = content;
    return cell;
  }

  function badge(text, kind) {
    return element("span", "badge " + (kind || "muted"), text);
  }

  function setBadge(target, text, kind) {
    target.className = "badge " + (kind || "muted");
    target.textContent = text;
  }

  function typeLabel(type) {
    var info = TYPE_INFO.find(function (item) { return item.type === type; });
    return info ? info.label : (type || "—");
  }

  function element(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  function setBusy(button, busy) {
    if (!button) return;
    if (busy) {
      button.disabled = true;
      button.setAttribute("aria-busy", "true");
    } else {
      button.disabled = false;
      button.removeAttribute("aria-busy");
    }
  }

  function showProviderFeedback(message, kind) {
    var target = document.getElementById("provider-feedback");
    target.hidden = false;
    target.className = "inline-feedback " + (kind || "info");
    target.textContent = message;
  }

  function showGlobalFeedback(message, kind) {
    var target = document.getElementById("global-feedback");
    if (!target) return;
    target.hidden = false;
    target.className = "global-feedback " + (kind || "info");
    target.textContent = message;
  }

  function humanError(error) {
    var message = (error && error.message) || "The request could not be completed.";
    return message.replace(/^configtx:\s*/i, "").replace(/^webadmin:\s*/i, "");
  }

  function shortRevision(revision) {
    return revision && revision.length > 12 ? revision.slice(0, 12) : revision;
  }

  function formatTimestamp(value) {
    var date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString([], { dateStyle: "medium", timeStyle: "short" });
  }

  var initialTab = window.location.hash.slice(1);
  showTab(isKnownTab(initialTab) ? initialTab : "overview", false);
})();
