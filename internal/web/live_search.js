(() => {
  const detailsAnimations = new WeakMap();
  const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

  if (!reducedMotion.matches && typeof Element.prototype.animate === "function") {
    document.addEventListener("click", (event) => {
      const target = event.target instanceof Element ? event.target : event.target.parentElement;
      const summary = target?.closest("summary");
      if (!summary || !(summary.parentElement instanceof HTMLDetailsElement)) return;
      const details = summary.parentElement;
      const interactive = target.closest("a, button, input, select, textarea");
      if (interactive && interactive !== summary) return;

      event.preventDefault();
      detailsAnimations.get(details)?.height.finish();

      const opening = !details.open;
      const startHeight = details.getBoundingClientRect().height;
      const collapsedHeight = summary.getBoundingClientRect().height + details.offsetHeight - details.clientHeight;
      let endHeight;
      if (opening) {
        details.open = true;
        endHeight = details.getBoundingClientRect().height;
      } else {
        endHeight = collapsedHeight;
      }

      const content = Array.from(details.children).filter((child) => child !== summary);
      details.dataset.animating = opening ? "opening" : "closing";
      details.style.overflow = "hidden";
      const contentAnimations = content.map((child) => child.animate(
        opening
          ? [{opacity: 0, transform: "translateY(-.25rem)"}, {opacity: 1, transform: "translateY(0)"}]
          : [{opacity: 1, transform: "translateY(0)"}, {opacity: 0, transform: "translateY(-.2rem)"}],
        {duration: opening ? 180 : 140, delay: opening ? 35 : 0, easing: "ease-out", fill: "both"},
      ));
      const height = details.animate(
        [{height: `${startHeight}px`}, {height: `${endHeight}px`}],
        {duration: opening ? 220 : 180, easing: "cubic-bezier(.2,.8,.2,1)"},
      );
      const state = {height, contentAnimations, opening};
      detailsAnimations.set(details, state);
      height.onfinish = () => {
        if (detailsAnimations.get(details) !== state) return;
        if (!opening) details.open = false;
        details.style.overflow = "";
        delete details.dataset.animating;
        contentAnimations.forEach((animation) => animation.cancel());
        detailsAnimations.delete(details);
      };
    });
  }

  document.querySelectorAll("[data-cast-carousel]").forEach((carousel) => {
    const list = carousel.querySelector("[data-cast-list]");
    const progress = carousel.querySelector("[data-cast-progress]");
    const progressDots = Array.from(carousel.querySelectorAll("[data-cast-progress-dot]"));
    if (!list) return;

    function updateProgress() {
      const maxScroll = Math.max(0, list.scrollWidth - list.clientWidth);
      const scrollable = maxScroll > 1;
      if (progress) progress.hidden = !scrollable;
      const active = maxScroll === 0 ? 0 : Math.min(progressDots.length - 1, Math.round((list.scrollLeft / maxScroll) * (progressDots.length - 1)));
      progressDots.forEach((dot, index) => dot.classList.toggle("is-active", index === active));
    }

    list.addEventListener("scroll", updateProgress, {passive: true});
    if (typeof ResizeObserver !== "undefined") new ResizeObserver(updateProgress).observe(list);
    window.addEventListener("resize", updateProgress);
    requestAnimationFrame(updateProgress);
  });

  document.querySelectorAll("[data-progressive-list]").forEach((list) => {
    let loading = false;

    async function loadMore(marker) {
      const offset = Number(marker.dataset.offset);
      if (!Number.isSafeInteger(offset) || offset < 0) return;
      loading = true;
      marker.textContent = `Loading more ${marker.dataset.label || "titles"}…`;
      const url = new URL(marker.dataset.endpoint, window.location.href);
      url.searchParams.set("offset", String(offset));
      try {
        const response = await fetch(url, {headers: {Accept: "text/html"}});
        if (!response.ok) throw new Error("more appearances unavailable");
        const fragment = document.createElement("template");
        fragment.innerHTML = await response.text();
        if (!marker.isConnected) return;
        const items = Array.from(fragment.content.children);
        list.querySelector("[data-progressive-items]")?.append(...items);
        if (items.length > 0 && response.headers.get("X-Has-More") === "true") {
          marker.dataset.offset = String(offset + items.length);
          marker.textContent = "";
        } else {
          marker.remove();
        }
      } catch {
        if (marker.isConnected) marker.textContent = `Could not load more ${marker.dataset.label || "titles"}. Scroll to retry.`;
      } finally {
        loading = false;
        if (marker.isConnected && marker.textContent === "") requestAnimationFrame(checkMore);
      }
    }

    function checkMore() {
      const marker = list.querySelector("[data-progressive-load]");
      if (!marker || loading) return;
      if (marker.getBoundingClientRect().top <= window.innerHeight + 300) loadMore(marker);
    }

    window.addEventListener("scroll", checkMore, {passive: true});
    window.addEventListener("resize", checkMore);
    requestAnimationFrame(checkMore);
  });

  const watchlistRandomizer = document.querySelector("[data-watchlist-randomizer]");
  const randomizerStatus = document.querySelector("[data-watchlist-randomizer-status]");
  let resetWatchlistRandomizer = () => {};
  if (watchlistRandomizer) {
    let selectedTitle = null;

    resetWatchlistRandomizer = () => {
      selectedTitle = null;
      watchlistRandomizer.disabled = !document.querySelector("[data-watchlist-titles] > .watchlist-title");
      watchlistRandomizer.removeAttribute("aria-label");
      if (randomizerStatus) randomizerStatus.textContent = "";
    };

    watchlistRandomizer.addEventListener("click", () => {
      const titles = Array.from(document.querySelectorAll("[data-watchlist-titles] > .watchlist-title"));
      if (titles.length === 0) return;
      const selectedIndex = titles.indexOf(selectedTitle);
      let nextIndex = Math.floor(Math.random() * titles.length);
      if (titles.length > 1 && nextIndex === selectedIndex) nextIndex = (nextIndex + 1) % titles.length;
      selectedTitle = titles[nextIndex];
      titles.forEach((title, index) => {
        title.hidden = index !== nextIndex;
      });
      const selectedName = selectedTitle.querySelector(".person-title-name")?.textContent?.trim() || "a title";
      watchlistRandomizer.setAttribute("aria-label", `Choose another title. Currently selected: ${selectedName}`);
      if (randomizerStatus) randomizerStatus.textContent = `Randomly selected ${selectedName}.`;
    });
  }

  const watchlistFilters = document.querySelector("[data-watchlist-filters]");
  const watchlistResults = document.querySelector("[data-watchlist-results]");
  if (watchlistFilters && watchlistResults) {
    let filterRequest;

    async function updateWatchlistFilter(url, pushHistory) {
      filterRequest?.abort();
      const request = new AbortController();
      filterRequest = request;
      watchlistResults.setAttribute("aria-busy", "true");
      try {
        const response = await fetch(url, {headers: {Accept: "text/html"}, signal: request.signal});
        if (!response.ok) throw new Error("watchlist filter unavailable");
        const page = new DOMParser().parseFromString(await response.text(), "text/html");
        const incomingResults = page.querySelector("[data-watchlist-results]");
        const incomingFilters = page.querySelector("[data-watchlist-filters]");
        if (!incomingResults || !incomingFilters || filterRequest !== request) return;
        watchlistResults.innerHTML = incomingResults.innerHTML;
        const currentLinks = Array.from(watchlistFilters.querySelectorAll("a"));
        const incomingLinks = Array.from(incomingFilters.querySelectorAll("a"));
        currentLinks.forEach((link, index) => {
          if (incomingLinks[index]?.hasAttribute("aria-current")) link.setAttribute("aria-current", "page");
          else link.removeAttribute("aria-current");
        });
        resetWatchlistRandomizer();
        if (pushHistory) window.history.pushState(null, "", url.pathname + url.search);
      } catch (error) {
        if (error.name !== "AbortError") window.location.assign(url.href);
      } finally {
        if (filterRequest === request) {
          filterRequest = null;
          watchlistResults.removeAttribute("aria-busy");
        }
      }
    }

    watchlistFilters.addEventListener("click", (event) => {
      const target = event.target instanceof Element ? event.target : event.target.parentElement;
      const link = target?.closest("a");
      if (!link || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      updateWatchlistFilter(new URL(link.href), true);
    });
    window.addEventListener("popstate", () => {
      const url = new URL(window.location.href);
      if (url.pathname !== "/watchlist") {
        window.location.reload();
        return;
      }
      updateWatchlistFilter(url, false);
    });
  }

  document.addEventListener("submit", async (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement) || !form.matches("[data-inline-title-action]")) return;
    const submitter = event.submitter;
    const stateName = submitter?.dataset.titleAction;
    if (!stateName || form.dataset.submitting === "true") return;
    event.preventDefault();

    const entry = form.closest("[data-title-entry]");
    const actions = entry?.querySelector("[data-title-actions]");
    const state = entry?.querySelector("[data-title-state]");
    const actionStatus = entry?.querySelector("[data-title-action-status]");
    const actionRegion = entry?.querySelector("[data-title-action-region]");
    const replacementAction = entry?.querySelector(`[data-title-remove="${stateName}"]`);
    if (!entry || !actions || !state) return;

    form.dataset.submitting = "true";
    actions.querySelectorAll("button, .button").forEach((control) => {
      if (control instanceof HTMLButtonElement) control.disabled = true;
      control.setAttribute("aria-disabled", "true");
    });
    if (actionStatus) actionStatus.textContent = stateName === "watched" ? "Adding to watched…" : "Adding to watchlist…";

    let titleAdded = false;
    try {
      const actionURL = submitter.getAttribute("formaction") || form.action;
      const response = await fetch(actionURL, {
        method: "POST",
        body: new FormData(form),
        headers: {Accept: "application/json"},
      });
      if (!response.ok) throw new Error("title action failed");
      titleAdded = true;
      const initialRegionHeight = actionRegion?.getBoundingClientRect().height;
      const tag = document.createElement("span");
      tag.className = `title-state-tag ${stateName === "watched" ? "watched-tag" : "watchlisted-tag"}`;
      tag.textContent = stateName === "watched" ? "Watched" : "Watchlisted";
      state.replaceChildren(tag);
      state.hidden = false;
      if (actionStatus) actionStatus.textContent = "";

      if (!reducedMotion.matches && typeof Element.prototype.animate === "function" && actionRegion && replacementAction) {
        const startRegionHeight = initialRegionHeight ?? actionRegion.getBoundingClientRect().height;
        replacementAction.hidden = false;
        actions.hidden = true;
        const endRegionHeight = actionRegion.getBoundingClientRect().height;
        actions.hidden = false;
        actionRegion.style.height = `${startRegionHeight}px`;
        actionRegion.style.overflow = "hidden";

        const transitions = [
          actions.animate(
            [{opacity: 1, transform: "translateY(0)"}, {opacity: 0, transform: "translateY(-.2rem)"}],
            {duration: 220, easing: "ease-out", fill: "forwards"},
          ),
          replacementAction.animate(
            [{opacity: 0, transform: "translateY(.2rem)"}, {opacity: 1, transform: "translateY(0)"}],
            {duration: 220, easing: "ease-out", fill: "forwards"},
          ),
          state.animate(
          [{opacity: 0, transform: "translateY(.2rem)"}, {opacity: 1, transform: "translateY(0)"}],
            {duration: 220, easing: "ease-out"},
          ),
          actionRegion.animate(
            [{height: `${startRegionHeight}px`}, {height: `${endRegionHeight}px`}],
            {duration: 220, easing: "cubic-bezier(.2,.8,.2,1)"},
          ),
        ];
        await Promise.allSettled(transitions.map((transition) => transition.finished));
        actions.hidden = true;
        actionRegion.style.height = "";
        actionRegion.style.overflow = "";
        actions.getAnimations().forEach((animation) => animation.cancel());
        replacementAction.getAnimations().forEach((animation) => animation.cancel());
      } else {
        actions.hidden = true;
        if (replacementAction) replacementAction.hidden = false;
        if (!reducedMotion.matches && typeof Element.prototype.animate === "function") {
          state.animate(
            [{opacity: 0, transform: "translateY(.2rem)"}, {opacity: 1, transform: "translateY(0)"}],
            {duration: 180, easing: "ease-out"},
          );
        }
      }
    } catch {
      if (titleAdded) {
        actions.hidden = true;
        if (replacementAction) replacementAction.hidden = false;
        if (actionRegion) {
          actionRegion.style.height = "";
          actionRegion.style.overflow = "";
        }
        if (actionStatus) actionStatus.textContent = "";
        return;
      }
      delete form.dataset.submitting;
      actions.querySelectorAll("button, .button").forEach((control) => {
        if (control instanceof HTMLButtonElement) control.disabled = false;
        control.removeAttribute("aria-disabled");
      });
      if (actionStatus) actionStatus.textContent = "Could not add this title. Please try again.";
    }
  });

  const form = document.querySelector("form[data-live-search]");
  const results = document.querySelector("#live-results");
  const status = document.querySelector("#search-status");
  if (!form || !results || !status) return;

  const input = form.querySelector('input[name="q"]');
  if (!input) return;
  const media = form.querySelector('input[name="media"]')?.value || "";

  let timer;
  let request;
  let sequence = 0;
  const mode = form.dataset.liveSearch;

  let loadingMore = false;
  async function loadMoreResults(marker) {
    const query = marker.dataset.query || "";
    const offset = Number(marker.dataset.offset);
    if (!Number.isSafeInteger(offset) || offset < 0) return;
    loadingMore = true;
    marker.textContent = `Loading more ${mode}…`;
    const url = new URL(marker.dataset.endpoint, window.location.href);
    url.searchParams.set("offset", String(offset));
    if (query) url.searchParams.set("q", query);
    try {
      const response = await fetch(url, {headers: {Accept: "text/html"}});
      if (!response.ok) throw new Error("more titles unavailable");
      const fragment = document.createElement("template");
      fragment.innerHTML = await response.text();
      if (!marker.isConnected || input.value.trim() !== query) return;
      const items = Array.from(fragment.content.children);
      results.querySelector(".item-list")?.append(...items);
      if (items.length > 0 && response.headers.get("X-Has-More") === "true") {
        marker.dataset.offset = String(offset + items.length);
        marker.textContent = "";
      } else {
        marker.remove();
      }
      status.textContent = "";
    } catch {
      if (marker.isConnected && input.value.trim() === query) {
        marker.textContent = `Could not load more ${mode}. Scroll to retry.`;
      }
    } finally {
      loadingMore = false;
      if (marker.isConnected && marker.textContent === "") requestAnimationFrame(checkMoreResults);
    }
  }

  function checkMoreResults() {
    if (mode !== "titles" && mode !== "people" || loadingMore || results.hasAttribute("aria-busy")) return;
    const marker = results.querySelector("[data-load-more-results]");
	if (!marker || marker.dataset.query !== input.value.trim()) return;
	if (marker.getBoundingClientRect().top <= window.innerHeight + 300) loadMoreResults(marker);
  }

  if (mode === "titles" || mode === "people") {
    window.addEventListener("scroll", checkMoreResults, {passive: true});
    window.addEventListener("resize", checkMoreResults);
    requestAnimationFrame(checkMoreResults);
  }

  input.addEventListener("input", () => {
    const query = input.value.trim();
    const current = ++sequence;
    clearTimeout(timer);
    if (request) request.abort();
    if (mode === "add") document.querySelector("main > p.error")?.remove();

    const url = new URL(form.action, window.location.href);
    if (media) url.searchParams.set("media", media);
    if (query) url.searchParams.set("q", query);
    else url.searchParams.delete("q");
    window.history.replaceState(null, "", url.pathname + url.search);

    if (!query && (mode === "people" || mode === "add")) {
      results.replaceChildren();
      results.removeAttribute("aria-busy");
      status.textContent = "";
      return;
    }
    results.setAttribute("aria-busy", "true");
    status.textContent = query ? "Searching…" : "Loading titles…";
    timer = window.setTimeout(async () => {
      request = new AbortController();
      try {
        const response = await fetch(url, {signal: request.signal, headers: {Accept: "text/html"}});
        if (!response.ok) throw new Error("search failed");
        const page = new DOMParser().parseFromString(await response.text(), "text/html");
        const next = page.querySelector("#live-results");
        if (!next) throw new Error("search results missing");
        if (current !== sequence) return;
        results.replaceChildren(...Array.from(next.childNodes, node => document.importNode(node, true)));
        const pageError = page.querySelector("main > p.error");
        status.textContent = pageError ? pageError.textContent : "";
		if (mode === "titles" || mode === "people") requestAnimationFrame(checkMoreResults);
      } catch (error) {
        if (current !== sequence || error.name === "AbortError") return;
        results.replaceChildren();
        status.textContent = "Search unavailable. Try typing again.";
      } finally {
        if (current === sequence) {
          results.removeAttribute("aria-busy");
          request = undefined;
        }
      }
    }, 350);
  });
})();
