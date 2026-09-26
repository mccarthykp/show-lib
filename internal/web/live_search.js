(() => {
  const form = document.querySelector("form[data-live-search]");
  const results = document.querySelector("#live-results");
  const status = document.querySelector("#search-status");
  if (!form || !results || !status) return;

  const input = form.querySelector('input[name="q"]');
  if (!input) return;

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
        status.textContent = "Search unavailable. Press Search to retry.";
      } finally {
        if (current === sequence) {
          results.removeAttribute("aria-busy");
          request = undefined;
        }
      }
    }, 350);
  });
})();
