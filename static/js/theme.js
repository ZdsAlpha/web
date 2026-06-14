// Toggle between light and dark, persisting the choice. The initial theme is
// applied inline in <head> to avoid a flash; this only handles the toggle.
(function () {
	var btn = document.getElementById("theme-toggle");
	if (!btn) return;
	btn.addEventListener("click", function () {
		var next = document.documentElement.getAttribute("data-theme") === "dark" ? "light" : "dark";
		document.documentElement.setAttribute("data-theme", next);
		localStorage.setItem("theme", next);
	});
})();
