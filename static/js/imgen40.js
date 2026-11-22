const run = document.querySelector("#generator-run");
const status = document.querySelector("#generator-status");
const progress = document.querySelector("#generator-progress");
const heading = document.querySelector("#generator-gallery-title");
const channel = document.querySelector("#generator-channel");
const tiles = [...document.querySelectorAll(".generator-grid figure")].map((figure) => ({
	preview: figure.querySelector("img"),
	canvas: figure.querySelector("canvas"),
}));
const channelIndexes = { red: 0, green: 1, blue: 2 };
const modelURL = run.dataset.modelUrl;
const modelSize = Number(run.dataset.modelBytes);

let worker;
let generated;
let examples;

run.disabled = !modelURL || modelSize <= 0;
channel.disabled = false;

run.addEventListener("click", () => {
	run.disabled = true;
	status.textContent = "Loading the generator…";
	try {
		worker ??= startWorker();
		worker.postMessage({ type: "generate", modelURL, modelSize });
	} catch {
		workerFailed();
	}
});

channel.addEventListener("change", () => {
	renderGallery().catch(() => {
		status.textContent = "Couldn’t read the example images. Try reloading the page.";
	});
});

function startWorker() {
	const instance = new Worker(new URL("./imgen40-worker.js", import.meta.url), { type: "module" });
	instance.addEventListener("error", workerFailed);
	instance.addEventListener("message", async ({ data }) => {
		if (data.type === "progress") {
			progress.hidden = false;
			progress.value = data.value;
			return;
		}
		progress.hidden = true;
		if (data.type === "images") {
			try {
				generated = data.images.map((buffer, index) => {
					const { width, height } = tiles[index].canvas;
					return new ImageData(new Uint8ClampedArray(buffer), width, height);
				});
				await renderGallery();
				heading.textContent = "Generated batch";
				status.textContent = "Generated 4 images.";
			} catch {
				status.textContent = "Couldn’t display the generated images.";
			} finally {
				run.disabled = false;
			}
		} else {
			status.textContent = data.text;
			if (data.type === "error") run.disabled = false;
		}
	});
	return instance;
}

function workerFailed() {
	worker?.terminate();
	worker = undefined;
	progress.hidden = true;
	run.disabled = false;
	status.textContent = "Couldn’t start the generator in this browser.";
}

async function readExample(tileslot) {
	const { preview, canvas } = tileslot;
	await preview.decode();
	const context = canvas.getContext("2d", { willReadFrequently: true });
	context.drawImage(preview, 0, 0);
	return context.getImageData(0, 0, canvas.width, canvas.height);
}

async function renderGallery() {
	if (!generated && channel.value !== "rgb") {
		examples ??= Promise.all(tiles.map(readExample)).catch((error) => {
			examples = undefined;
			throw error;
		});
	}
	const samples = generated ?? (channel.value !== "rgb" ? await examples : undefined);
	// A generation or channel change could have finished while examples loaded.
	const images = generated ?? samples;
	const mode = channel.value;
	const useCanvas = Boolean(generated) || mode !== "rgb";

	tiles.forEach(({ preview, canvas }, index) => {
		if (useCanvas) {
			const output = images[index];
			const pixels = mode === "rgb" ? output : grayscale(output, channelIndexes[mode]);
			canvas.getContext("2d").putImageData(pixels, 0, 0);
			const description = mode === "rgb" ? "RGB" : `${mode} channel`;
			canvas.setAttribute("aria-label", `Portrait ${index + 1}, ${description}`);
		}
		canvas.hidden = !useCanvas;
		preview.hidden = useCanvas;
	});
}

function grayscale(image, channelIndex) {
	const pixels = new Uint8ClampedArray(image.data);
	for (let pixel = 0; pixel < pixels.length; pixel += 4) {
		const value = pixels[pixel + channelIndex];
		pixels[pixel] = value;
		pixels[pixel + 1] = value;
		pixels[pixel + 2] = value;
	}
	return new ImageData(pixels, image.width, image.height);
}
