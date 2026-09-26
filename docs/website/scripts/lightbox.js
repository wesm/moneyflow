const dialog = document.createElement("dialog");
dialog.className = "lightbox";
dialog.setAttribute("aria-label", "Application screenshot");
dialog.innerHTML = `<div class="lightbox-toolbar"><p></p><a target="_blank" rel="noopener">Open original ↗</a><button type="button" autofocus aria-label="Close screenshot">Close ×</button></div><div class="lightbox-image"><img alt=""></div>`;
document.body.append(dialog);
let opener;

document.querySelectorAll("a[data-lightbox]").forEach((link) => {
  link.addEventListener("click", (event) => {
    if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey)
      return;
    event.preventDefault();
    opener = link;
    const image = link.querySelector("img");
    dialog.querySelector("img").src = link.getAttribute("href");
    dialog.querySelector("img").alt = image.alt;
    dialog.querySelector("p").textContent = image.alt;
    dialog.querySelector("a").href = link.href;
    dialog.showModal();
  });
});
dialog.querySelector("button").addEventListener("click", () => dialog.close());
dialog.addEventListener("click", (event) => {
  if (event.target !== dialog) return;
  const bounds = dialog.getBoundingClientRect();
  if (
    event.clientX < bounds.left ||
    event.clientX > bounds.right ||
    event.clientY < bounds.top ||
    event.clientY > bounds.bottom
  )
    dialog.close();
});
dialog.addEventListener("close", () => opener?.focus());
