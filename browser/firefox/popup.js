const content = document.querySelector("#content");
const refreshButton = document.querySelector("#refresh");
const age = document.querySelector("#age");
const notice = document.querySelector("#notice");

async function nativeRequest(request) {
  const response = await browser.runtime.sendMessage({
    destination: "workdash-native",
    ...request,
  });
  if (!response || !response.ok) {
    throw new Error(response?.error || "Workdash did not return a response");
  }
  return response;
}

function setStatus(message, className = "status") {
  content.replaceChildren();
  const node = document.createElement("p");
  node.className = className;
  node.textContent = message;
  content.append(node);
}

function setNotice(message = "", error = false) {
  notice.textContent = message;
  notice.className = error ? "error" : "";
}

function formatLoadedAt(value, refreshing) {
  if (!value) {
    return refreshing ? "Refreshing…" : "";
  }
  const loaded = new Date(value);
  const text = `Loaded ${loaded.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;
  return refreshing ? `${text} · refreshing…` : text;
}

function render(response) {
  content.replaceChildren();
  setNotice();
  age.textContent = formatLoadedAt(response.loaded_at, response.refreshing);
  for (const group of response.groups || []) {
    const section = document.createElement("section");
    section.className = "group";

    const title = document.createElement("div");
    title.className = "group-title";
    const kind = group.kind === "worktree" ? "workspace" : group.kind;
    title.textContent = `${group.host || "local"} · ${kind} · ${group.title}`;
    const subtitle = document.createElement("div");
    subtitle.className = "group-subtitle";
    subtitle.textContent = group.path || group.subtitle || "";
    title.append(subtitle);
    section.append(title);

    for (const action of group.actions) {
      const button = document.createElement("button");
      button.className = "action";
      button.disabled = !action.enabled;
      const label = document.createElement("span");
      label.className = "action-label";
      label.textContent = action.label;
      button.append(label);
      const detail = document.createElement("span");
      detail.className = "detail";
      detail.textContent = action.enabled
        ? action.detail || ""
        : action.unavailable_reason || "Unavailable";
      button.append(detail);
      button.addEventListener("click", () => execute(action, button));
      section.append(button);
    }
    content.append(section);
  }
}

async function loadActions() {
  setNotice();
  setStatus("Loading Workdash actions…");
  try {
    const [tab] = await browser.tabs.query({ active: true, currentWindow: true });
    const response = await nativeRequest({
      type: "get_pr_actions",
      url: tab?.url || "",
    });
    render(response);
  } catch (error) {
    setStatus(error.message, "error");
  }
}

async function execute(action, button) {
  let confirmed = false;
  if (action.confirmation_title) {
    confirmed = window.confirm(
      `${action.confirmation_title}\n\n${action.confirmation_detail || ""}`,
    );
    if (!confirmed) {
      return;
    }
  }
  button.disabled = true;
  const label = button.querySelector(".action-label");
  const previous = label.textContent;
  label.textContent = "Running…";
  try {
    await nativeRequest({
      type: "execute_action",
      token: action.token,
      confirmed,
    });
    label.textContent = "Completed";
    button.classList.add("success");
  } catch (error) {
    label.textContent = previous;
    button.disabled = false;
    setNotice(error.message, true);
  }
}

refreshButton.addEventListener("click", async () => {
  refreshButton.disabled = true;
  setStatus("Refreshing Workdash data…");
  try {
    await nativeRequest({ type: "refresh_browser" });
    await loadActions();
  } catch (error) {
    setStatus(error.message, "error");
  } finally {
    refreshButton.disabled = false;
  }
});

loadActions();
