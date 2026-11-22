import * as ort from "../ort/1.31.1/ort.wasm.min.mjs";

const imageSize = 256;
const batchSize = 4;

ort.env.wasm.numThreads = 1;
ort.env.wasm.wasmPaths = new URL("../ort/1.31.1/", import.meta.url).href;

let session;
let running = false;

self.addEventListener("message", async (event) => {
	if (event.data?.type !== "generate" || running) return;
	running = true;
	try {
		const { modelURL, modelSize } = event.data;
		session ??= await loadSession(modelURL, modelSize);
		const images = [];
		// Run one portrait at a time to keep intermediate tensors small.
		for (let index = 0; index < batchSize; index++) {
			status(`Generating image ${index + 1} of ${batchSize}…`);
			const z = new ort.Tensor("float32", gaussianNoise(512), [1, 512]);
			try {
				const { image } = await session.run({ z });
				try {
					images.push(toPixels(image.data).buffer);
				} finally {
					image.dispose();
				}
			} finally {
				z.dispose();
			}
		}
		self.postMessage({ type: "images", images }, images);
	} catch (error) {
		console.error("imgen40", error);
		self.postMessage({
			type: "error",
			text: error instanceof ModelDownloadError
				? error.message
				: "The generator couldn’t run in this browser. Try again or use another browser.",
		});
	} finally {
		running = false;
	}
});

async function loadSession(modelURL, modelSize) {
	const model = await loadModel(modelURL, modelSize);
	status("Initializing the generator…");
	return ort.InferenceSession.create(model, { executionProviders: ["wasm"] });
}

class ModelDownloadError extends Error {}

async function loadModel(modelURL, modelSize) {
	const key = new URL(modelURL, self.location.origin).href;
	const cache = await self.caches?.open("arehman-imgen40").catch(() => null);
	const saved = await cache?.match(key).catch(() => null);
	const cachedBytes = await saved?.arrayBuffer().catch(() => null);
	if (cachedBytes?.byteLength === modelSize) {
		return new Uint8Array(cachedBytes);
	}
	if (saved) await cache.delete(key).catch(() => {});

	status("Downloading the model…");
	let response;
	try {
		response = await fetch(key);
	} catch {
		throw new ModelDownloadError("The model couldn’t be downloaded. Check your connection and try again.");
	}
	if (!response.ok || !response.body) {
		throw new ModelDownloadError("The model download is unavailable. The example portraits are still available.");
	}

	const write = cache?.put(key, response.clone()).then(() => true, () => false);
	try {
		const bytes = new Uint8Array(modelSize);
		const reader = response.body.getReader();
		let loaded = 0;
		let percent = 0;
		try {
			for (;;) {
				const { value, done } = await reader.read();
				if (done) break;
				bytes.set(value, loaded);
				loaded += value.byteLength;
				const nextPercent = Math.round(100 * loaded / modelSize);
				if (nextPercent !== percent) {
					percent = nextPercent;
					self.postMessage({ type: "progress", value: percent });
				}
			}
		} finally {
			await reader.cancel().catch(() => {});
		}
		if (loaded !== modelSize) throw new Error("Unexpected model size");
		if (await write) {
			const keys = await cache.keys().catch(() => []);
			await Promise.allSettled(keys.filter((entry) => entry.url !== key).map((entry) => cache.delete(entry)));
		}
		return bytes;
	} catch {
		await write;
		await cache?.delete(key).catch(() => {});
		throw new ModelDownloadError("The model download was incomplete. Try again.");
	}
}

function status(text) {
	self.postMessage({ type: "status", text });
}

function gaussianNoise(count) {
	const noise = new Float32Array(count);
	for (let index = 0; index < count; index += 2) {
		const radius = Math.sqrt(-2 * Math.log(1 - Math.random()));
		const angle = 2 * Math.PI * Math.random();
		noise[index] = radius * Math.cos(angle);
		noise[index + 1] = radius * Math.sin(angle);
	}
	return noise;
}

function toPixels(output) {
	const plane = imageSize * imageSize;
	const rgba = new Uint8ClampedArray(4 * plane);
	for (let pixel = 0; pixel < plane; pixel++) {
		rgba[4 * pixel] = output[pixel] * 255;
		rgba[4 * pixel + 1] = output[plane + pixel] * 255;
		rgba[4 * pixel + 2] = output[2 * plane + pixel] * 255;
		rgba[4 * pixel + 3] = 255;
	}
	return rgba;
}
