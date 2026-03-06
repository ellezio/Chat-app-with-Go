function selectChat(id) {
  window.chatId = id;
  history.pushState(null, "", "/chat/" + id);
}

function msgScroller() {
  const elt = document.getElementById("scroller");
  const msgsList = document.getElementById("msgs-list");
  const sharedState = {
    anchored: true,
    autoScroll: false,
  };

  elt.addEventListener("scroll", (evt) => {
    if (!sharedState.autoScroll) {
      sharedState.anchored =
        evt.target.scrollTop >=
        evt.target.scrollHeight - evt.target.offsetHeight - 10;
    }
    sharedState.autoScroll = false;
  });

  const observer = new ResizeObserver((entries) => {
    for (const _entry of entries) {
      if (sharedState.anchored) {
        elt.scrollTop = elt.scrollHeight - elt.offsetHeight;
        sharedState.autoScroll = true;
      }
    }
  });

  observer.observe(msgsList);
}

function handleSetPosition(elm, relativeTo, ctxMenu) {
  const rect = relativeTo.getBoundingClientRect();
  const isAuthor =
    relativeTo.classList.contains("left-0") ||
    relativeTo.classList.contains("-translate-x-full");

  if (isAuthor) {
    elm.style.top = (rect.top || rect.y) + "px";
    elm.style.left = (rect.left || rect.x) - ctxMenu.offsetWidth + "px";
  } else {
    elm.style.top = (rect.top || rect.y) + "px";
    elm.style.left = (rect.left || rect.x) + rect.width + "px";
  }
}

function toggleContextMenu(event, show) {
  document.querySelectorAll(".ctx-menu:not(.hidden)")?.forEach((ctxMenu) => {
    ctxMenu.classList.toggle("hidden", true);
  });

  const ctxMenusWrapper = document.getElementById("ctxMenusWrapper");
  ctxMenusWrapper.classList.toggle("hidden", true);
  if (!show) return;

  const msgId = event?.currentTarget.getAttribute("data-msg-id");
  if (!msgId) return;

  const ctxMenu = document.getElementById(`ctx-menu-${msgId}`);
  ctxMenu.classList.toggle("hidden", false);
  ctxMenusWrapper.classList.toggle("hidden", false);

  handleSetPosition(ctxMenu, event.currentTarget, ctxMenu);
}

window.addEventListener("resize", () => {
  const ctxMenu = document.querySelector(".ctx-menu:not(.hidden)");
  if (!ctxMenu) return;

  const msgId = ctxMenu.getAttribute("data-msg-id");
  const invokedFrom = document.getElementById("ctx-menu-invoker-" + msgId);

  handleSetPosition(ctxMenu, invokedFrom, ctxMenu);
});
