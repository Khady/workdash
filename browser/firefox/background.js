const nativeHost = "io.github.khady.workdash";
const cacheTTL = 60 * 1000;
const actionCache = new Map();
const summaryCache = new Map();
const pendingActions = new Map();
const pendingSummaries = new Map();
let warmupPromise;

function canonicalPRURL(rawURL) {
  try {
    const url = new URL(rawURL);
    if (url.protocol !== "https:" || url.hostname !== "github.com") {
      return null;
    }
    const match = url.pathname.match(/^\/([^/]+)\/([^/]+)\/pull\/([1-9]\d*)(?:\/|$)/);
    if (!match) {
      return null;
    }
    return `https://github.com/${match[1].toLowerCase()}/${match[2].toLowerCase()}/pull/${match[3]}`;
  } catch {
    return null;
  }
}

async function sendNative(request) {
  const response = await browser.runtime.sendNativeMessage(nativeHost, request);
  if (!response || !response.ok) {
    throw new Error(response?.error || "Workdash did not return a response");
  }
  return response;
}

async function warmup() {
  if (!warmupPromise) {
    warmupPromise = sendNative({ type: "warmup" }).catch((error) => {
      warmupPromise = undefined;
      throw error;
    });
  }
  return warmupPromise;
}

async function cachedPRRequest(rawURL, options) {
  const canonical = canonicalPRURL(rawURL);
  if (!canonical) {
    throw new Error("Current page is not a github.com pull request");
  }
  const cached = options.cache.get(canonical);
  if (cached && Date.now() - cached.at < cacheTTL) {
    return cached.response;
  }
  if (options.pending.has(canonical)) {
    return options.pending.get(canonical);
  }
  const pending = (async () => {
    await warmup();
    const response = await sendNative({ type: options.type, url: canonical });
    options.cache.set(canonical, { at: Date.now(), response });
    return response;
  })();
  options.pending.set(canonical, pending);
  try {
    return await pending;
  } finally {
    options.pending.delete(canonical);
  }
}

function actionsForURL(rawURL) {
  return cachedPRRequest(rawURL, {
    type: "get_pr_actions",
    cache: actionCache,
    pending: pendingActions,
  });
}

function summaryForURL(rawURL) {
  return cachedPRRequest(rawURL, {
    type: "get_pr_summary",
    cache: summaryCache,
    pending: pendingSummaries,
  });
}

async function clearBadge(tabId, title = "Workdash") {
  await Promise.all([
    browser.action.setBadgeText({ tabId, text: "" }),
    browser.action.setTitle({ tabId, title }),
  ]);
}

async function updateBadge(tabId, rawURL) {
  if (!canonicalPRURL(rawURL)) {
    await clearBadge(tabId);
    return;
  }
  await browser.action.setBadgeBackgroundColor({ tabId, color: "#c7f464" });
  await browser.action.setBadgeTextColor({ tabId, color: "#101310" });
  await browser.action.setBadgeText({ tabId, text: "…" });
  await browser.action.setTitle({ tabId, title: "Workdash is checking this PR" });
  try {
    const response = await summaryForURL(rawURL);
    const count = response.action_count || 0;
    if (count === 0) {
      await clearBadge(tabId, "Workdash has no actions for this PR");
      return;
    }
    await browser.action.setBadgeText({ tabId, text: count > 99 ? "99+" : String(count) });
    await browser.action.setTitle({
      tabId,
      title: `Workdash: ${count} action${count === 1 ? "" : "s"} available`,
    });
  } catch {
    await clearBadge(tabId, "Workdash has no cached information for this PR");
  }
}

async function updateKnownTabs() {
  const tabs = await browser.tabs.query({ url: "https://github.com/*" });
  await Promise.all(tabs.map((tab) => updateBadge(tab.id, tab.url)));
}

async function warmAndAnnotate() {
  try {
    await warmup();
  } catch {
    // A later URL change or popup interaction retries the daemon connection.
  }
  await updateKnownTabs();
}

function clearCaches() {
  actionCache.clear();
  summaryCache.clear();
  pendingActions.clear();
  pendingSummaries.clear();
}

browser.runtime.onMessage.addListener(async (message) => {
  if (!message || message.destination !== "workdash-native") {
    return undefined;
  }
  const request = { ...message };
  delete request.destination;
  if (request.type === "get_pr_actions") {
    return actionsForURL(request.url);
  }
  const response = await sendNative(request);
  if (request.type === "refresh_browser" || request.type === "execute_action") {
    clearCaches();
  }
  if (request.type === "refresh_browser") {
    void updateKnownTabs();
  }
  return response;
});

// URL changes are the only ongoing automatic trigger. Merely focusing Firefox,
// switching tabs, or interacting with a page does no Workdash work.
browser.tabs.onUpdated.addListener(
  (tabId, changeInfo, tab) => {
    if (changeInfo.url) {
      void updateBadge(tabId, tab.url);
    }
  },
  {
    properties: ["url"],
    urls: ["https://github.com/*"],
  },
);

browser.runtime.onStartup.addListener(() => void warmAndAnnotate());
browser.runtime.onInstalled.addListener(() => void warmAndAnnotate());
