// fleet Chrome Extension Service Worker
// Connects to native messaging host and manages tabs/groups on command.

const NATIVE_HOST = "com.brizzai.fleet.tabcontrol";
let port = null;

function connect() {
  try {
    port = chrome.runtime.connectNative(NATIVE_HOST);
  } catch (e) {
    console.error("fleet: failed to connect to native host:", e);
    scheduleReconnect();
    return;
  }

  port.onMessage.addListener((msg) => {
    handleCommand(msg);
  });

  port.onDisconnect.addListener(() => {
    const err = chrome.runtime.lastError;
    console.log("fleet: native host disconnected", err ? err.message : "");
    port = null;
    scheduleReconnect();
  });

  console.log("fleet: connected to native host");
}

function scheduleReconnect() {
  setTimeout(() => {
    if (!port) {
      connect();
    }
  }, 2000);
}

function sendResponse(resp) {
  if (port) {
    try {
      port.postMessage(resp);
    } catch (e) {
      console.error("fleet: failed to send response:", e);
    }
  }
}

async function handleCommand(cmd) {
  const id = cmd.id || "";

  try {
    switch (cmd.action) {
      case "open_or_focus":
        await handleOpenOrFocus(id, cmd);
        break;
      case "close_tab":
        await handleCloseTab(id, cmd);
        break;
      case "create_tab_group":
        await handleCreateTabGroup(id, cmd);
        break;
      case "ping":
        sendResponse({ id, success: true, data: { pong: true } });
        break;
      default:
        sendResponse({
          id,
          success: false,
          error: `unknown action: ${cmd.action}`,
        });
    }
  } catch (e) {
    sendResponse({ id, success: false, error: e.message });
  }
}

async function handleOpenOrFocus(id, cmd) {
  const url = cmd.url;
  if (!url) {
    sendResponse({ id, success: false, error: "url is required" });
    return;
  }

  // Query for existing tab matching this URL (prefix match).
  const tabs = await chrome.tabs.query({ url: url + "*" });

  if (tabs.length > 0) {
    const tab = tabs[0];
    // Focus existing tab.
    await chrome.tabs.update(tab.id, { active: true });
    await chrome.windows.update(tab.windowId, { focused: true });
    sendResponse({ id, success: true, data: { reused: true, tabId: tab.id } });
    return;
  }

  // Create new tab.
  const tab = await chrome.tabs.create({ url });

  // Add to tab group if specified.
  if (cmd.group) {
    await ensureTabInGroup(tab.id, cmd.group, cmd.color);
  }

  sendResponse({
    id,
    success: true,
    data: { reused: false, tabId: tab.id },
  });
}

async function handleCloseTab(id, cmd) {
  const url = cmd.url;
  if (!url) {
    sendResponse({ id, success: false, error: "url is required" });
    return;
  }

  const tabs = await chrome.tabs.query({ url: url + "*" });
  if (tabs.length === 0) {
    sendResponse({ id, success: true, data: { closed: 0 } });
    return;
  }

  const tabIds = tabs.map((t) => t.id);
  await chrome.tabs.remove(tabIds);
  sendResponse({ id, success: true, data: { closed: tabIds.length } });
}

async function handleCreateTabGroup(id, cmd) {
  const name = cmd.name || cmd.group;
  if (!name) {
    sendResponse({ id, success: false, error: "name/group is required" });
    return;
  }

  // Find or create group.
  const groups = await chrome.tabGroups.query({ title: name });
  if (groups.length > 0) {
    sendResponse({
      id,
      success: true,
      data: { groupId: groups[0].id, created: false },
    });
    return;
  }

  // Create an empty group (need at least one tab).
  const tab = await chrome.tabs.create({ active: false });
  const groupId = await chrome.tabs.group({ tabIds: [tab.id] });

  const updateProps = { title: name };
  if (cmd.color) {
    updateProps.color = cmd.color;
  }
  await chrome.tabGroups.update(groupId, updateProps);

  sendResponse({ id, success: true, data: { groupId, created: true } });
}

async function ensureTabInGroup(tabId, groupName, color) {
  // Find existing group by name.
  const groups = await chrome.tabGroups.query({ title: groupName });

  if (groups.length > 0) {
    // Add tab to existing group.
    await chrome.tabs.group({ tabIds: [tabId], groupId: groups[0].id });
    return;
  }

  // Create new group with this tab.
  const groupId = await chrome.tabs.group({ tabIds: [tabId] });
  const updateProps = { title: groupName };
  if (color) {
    updateProps.color = color;
  }
  await chrome.tabGroups.update(groupId, updateProps);
}

// Connect on install/startup.
chrome.runtime.onInstalled.addListener(() => {
  connect();
});

chrome.runtime.onStartup.addListener(() => {
  connect();
});

// Also connect immediately (covers reload case).
connect();
