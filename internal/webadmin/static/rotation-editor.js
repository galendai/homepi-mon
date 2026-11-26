// The form edits product screens. Only the profile API uses comma-separated IDs.
function ids(csv) {
  return csv ? csv.split(",").map(id => id.trim()) : [];
}

function timings(settings) {
  return new Map(ids(settings.page_dwell_seconds).map(pair => pair.split(":")));
}

function lockOf(settings) {
  return settings.page_lock || (settings.home_only === "true" ? "HOME" : "");
}

function modeOf(settings) {
  return lockOf(settings) ? "lock" : settings.page_subset ? "subset" : "default";
}

// The default terminal router starts at CODING wherever it appears in the order.
function sequence(settings, renderer) {
  if (lockOf(settings)) return [lockOf(settings)];
  if (settings.page_subset) return ids(settings.page_subset);
  const order = ids(settings.page_order);
  if (renderer === "gui") return ["HOME", ...order];
  if (renderer === "terminal") {
    const first = Math.max(0, order.indexOf("CODING"));
    return order.slice(first).concat(order.slice(0, first));
  }
  return null;
}

function node(tag, className, text) {
  const el = document.createElement(tag);
  if (className) el.className = className;
  if (text !== undefined) el.textContent = text;
  return el;
}

export class RotationEditor {
  constructor(form, changed) {
    this.form = form;
    this.changed = changed;
    this.fields = form.elements;
    this.list = document.getElementById("rotation-list");
    this.available = document.getElementById("available-screens");
    this.defaultButton = document.getElementById("edit-default");
    this.remote = {};
    this.pages = new Map();
    form.querySelector(".startup-modes").addEventListener("change", () => {
      if (this.mode === "subset" && this.subset === null) {
        const startup = this.remote.startup;
        this.subset = (startup && sequence(startup, startup.renderer)) ||
          [...this.pages.values()].filter(page => page.native).map(page => page.id);
      }
      this.render();
    });
    this.defaultButton.addEventListener("click", () => {
      this.defaultExpanded = !this.defaultExpanded;
      this.render();
    });
    this.list.addEventListener("input", event => {
      const input = event.target;
      const id = input.dataset.dwell;
      if (!id) return;
      if (input.validity.valid) {
        this.times.set(id, input.value);
        this.incompleteTimes.delete(id);
      } else {
        this.incompleteTimes.set(id, input.value);
      }
    });
    this.list.addEventListener("click", event => this.changeList(event));
    this.available.addEventListener("click", event => this.changeList(event));
    document.getElementById("copy-startup").addEventListener("click", () => {
      if (this.remote.startup) {
        this.load(this.remote.startup);
        this.changed();
        this.form.querySelector("[name=rotation_mode]:checked").focus();
      }
    });
  }

  setCatalog(catalog, remoteSelect) {
    this.pages = new Map(catalog.map(page => [page.id, page]));
    for (const select of [this.fields.page_lock, remoteSelect]) {
      select.replaceChildren();
      for (const native of [true, false]) {
        const group = node("optgroup");
        group.label = native ? "Graphical screens" : "Fixed-grid screens";
        for (const page of catalog.filter(page => page.native === native)) {
          group.append(new Option(page.title, page.id));
        }
        select.append(group);
      }
    }
  }

  load(settings) {
    this.order = ids(settings.page_order);
    this.times = timings(settings);
    this.incompleteTimes = new Map();
    this.subset = settings.page_subset ? ids(settings.page_subset) : null;
    const mode = modeOf(settings);
    for (const radio of this.fields.rotation_mode) radio.checked = radio.value === mode;
    this.fields.page_lock.value = lockOf(settings) || "HOME";
    this.defaultExpanded = false;
    this.render();
  }

  get mode() { return this.fields.rotation_mode.value; }

  settings() {
    const lock = this.mode === "lock" ? this.fields.page_lock.value : "";
    return {
      page_order: this.order.join(","),
      page_dwell_seconds: this.order.map(id => id + ":" + this.times.get(id)).join(","),
      page_subset: this.mode === "subset" ? this.subset.join(",") : "",
      page_lock: lock === "HOME" ? "" : lock,
      home_only: lock === "HOME" ? "true" : "false"
    };
  }

  valid() {
    return Boolean(this.order && (this.mode !== "subset" || this.subset?.length) && !this.incompleteScreen());
  }

  incompleteScreen() {
    if (this.mode === "lock") return;
    return (this.mode === "subset" ? this.subset : this.order).find(id => this.incompleteTimes.has(id));
  }

  title(id, renderer) {
    const page = this.pages.get(id);
    return (renderer === "terminal" && page?.terminal_title) || page?.title || id;
  }

  seconds(id, times) {
    return this.pages.get(id)?.default_seconds || Number(times.get(id));
  }

  renderStartup(remote) {
    this.remote = remote;
    const startup = remote.startup;
    const target = document.getElementById("display-note");
    const source = document.getElementById("startup-source");
    const flags = (startup?.flags || []).map(flag => flag.split("=")[0]);
    const modeFlags = flags.filter(flag => ["-pages", "-page-only", "-home-only"].includes(flag));
    if (modeFlags.length) source.textContent = "Startup mode comes from " + modeFlags.join(", ") + " on the Pi. The service override takes priority over the saved mode.";
    else if (flags.length) source.textContent = "Launch settings on the Pi take priority for " + flags.join(", ") + ".";
    else if (remote.startup_state === "startup_differs") source.textContent = "The running process read different settings from the saved file.";
    else source.textContent = "";
    source.hidden = !source.textContent;
    target.replaceChildren();
    if (!startup) {
      target.textContent = remote.note || "The running kiosk’s startup settings could not be read.";
    } else {
      const times = timings(startup);
      const known = sequence(startup, startup.renderer);
      const layouts = known ? [startup.renderer] : ["gui", "terminal"];
      for (const renderer of layouts) {
        if (!known) target.append(node("strong", "layout-label", renderer === "gui" ? "Graphical kiosk" : "Terminal"));
        const strip = node("ol", "startup-sequence");
        for (const id of known || sequence(startup, renderer)) {
          const chip = node("li");
          chip.append(node("strong", "", this.title(id, renderer)),
            node("span", "", lockOf(startup) ? "Locked" : this.seconds(id, times) + " s"));
          strip.append(chip);
        }
        target.append(strip);
      }
    }
    this.preview();
  }

  action(action, id, text, label, disabled = false) {
    const button = node("button", "screen-action", text);
    button.type = "button";
    button.dataset.action = action;
    button.dataset.screen = id;
    button.setAttribute("aria-label", label);
    button.title = label;
    button.disabled = disabled;
    return button;
  }

  changeList(event) {
    const button = event.target.closest("button[data-action]");
    if (!button || button.disabled) return;
    const { screen, action } = button.dataset;
    const order = this.mode === "subset" ? this.subset : this.order;
    const index = order.indexOf(screen);
    if (action === "add") order.push(screen);
    else if (action === "remove") order.splice(index, 1);
    else {
      const other = index + (action === "earlier" ? -1 : 1);
      [order[index], order[other]] = [order[other], order[index]];
    }
    this.render();
    this.changed();
    const same = this.form.querySelectorAll("button[data-screen='" + screen + "']:not(:disabled)");
    const focus = [...same].find(item => item.dataset.action === action) || same[0];
    if (focus) focus.focus();
  }

  render() {
    const custom = this.mode === "subset";
    for (const radio of this.fields.rotation_mode) radio.parentElement.classList.toggle("selected", radio.checked);
    const order = custom ? this.subset : this.order;
    const showList = custom || (this.mode === "default" && this.defaultExpanded);
    this.list.replaceChildren();
    this.list.hidden = !showList || !order.length;
    this.defaultButton.hidden = this.mode !== "default";
    this.defaultButton.setAttribute("aria-expanded", String(this.defaultExpanded));
    this.defaultButton.textContent = this.defaultExpanded ? "Hide default order and times" : "Edit default order and times";
    document.getElementById("lock-row").hidden = this.mode !== "lock";
    document.getElementById("rotation-empty").hidden = !custom || Boolean(order.length);
    if (showList) {
      order.forEach((id, i) => {
        const page = this.pages.get(id);
        const row = node("li");
        const name = node("div", "screen-name");
        name.append(node("strong", "", page.title), node("small", "", page.description));
        const duration = node("div", "screen-duration");
        if (page.persistent) {
          const input = node("input");
          input.type = "number"; input.min = "5"; input.max = "300"; input.required = true;
          input.dataset.dwell = id;
          input.value = this.incompleteTimes.get(id) ?? this.times.get(id);
          input.setAttribute("aria-label", page.title + " seconds");
          duration.append(input, node("span", "", "s"));
        } else {
          duration.append(node("span", "fixed-time", page.default_seconds + " s · fixed"));
        }
        const actions = node("div", "screen-actions");
        actions.append(this.action("earlier", id, "↑", "Move " + page.title + " earlier", i === 0),
          this.action("later", id, "↓", "Move " + page.title + " later", i === order.length - 1));
        if (custom) actions.append(this.action("remove", id, "×", "Remove " + page.title));
        row.append(name, duration, actions);
        this.list.append(row);
      });
    }
    this.available.replaceChildren();
    if (custom) {
      for (const page of this.pages.values()) {
        if (!order.includes(page.id)) this.available.append(this.action("add", page.id, "+ " + page.title, "Add " + page.title));
      }
    }
    document.getElementById("rotation-add").hidden = !this.available.childElementCount;
    this.preview();
  }

  preview() {
    if (!this.order) return;
    const settings = this.settings();
    const summary = document.getElementById("display-preview");
    const lock = lockOf(settings);
    const renderer = this.remote.startup?.renderer;
    const incomplete = this.incompleteScreen();
    if (incomplete) summary.textContent = this.title(incomplete) + " needs a whole number from 5 to 300 seconds.";
    else if (lock) summary.textContent = "Stays on " + this.title(lock, renderer) + " until a remote command changes the screen.";
    else if (this.mode === "subset") {
      const total = this.subset.reduce((seconds, id) => seconds + this.seconds(id, this.times), 0);
      summary.textContent = total ? this.subset.length + (this.subset.length === 1 ? " screen" : " screens") + " · " + total + " s per cycle" : "";
    } else {
      const names = kind => sequence(settings, kind).map(id => this.title(id, kind)).join(" → ");
      summary.textContent = renderer ? names(renderer) : "Graphical · " + names("gui") + "\nTerminal · " + names("terminal");
    }
    const hint = document.getElementById("display-capability");
    hint.hidden = !(this.remote.verified && !this.remote.supports_page_env && (this.mode === "subset" || settings.page_lock));
    hint.textContent = hint.hidden ? "" : "Upgrade homepi-display on the Pi to apply this startup mode.";
  }
}
