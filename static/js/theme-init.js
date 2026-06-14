// Apply the saved/preferred theme before first paint to avoid a flash of the
// wrong color scheme. Loaded synchronously in <head>.
(function () {
	var t = localStorage.getItem("theme");
	if (!t) t = matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
	document.documentElement.setAttribute("data-theme", t);
})();
