// Browser-only S3 Zero workspace. Account credentials, bucket registrations,
// and master files stay in IndexedDB. S3 verification and listing are direct
// browser requests. Proxy-mode requests use only the isolated /vault/proxy/*
// endpoints and receive ciphertext; decryption remains in this file.
(function () {
	"use strict";

	var DB_NAME = "s3zero-local-vault";
	var DB_VERSION = 2;
	var STORE_NAME = "vaults";
	var LEGACY_STORE_NAME = "accounts";
	var MAX_IMPORT_BYTES = 32 * 1024 * 1024;
	var MAX_IMPORT_VAULTS = 500;
	var MAX_MASTER_BYTES = 8 * 1024 * 1024;
	var DEFAULT_ENDPOINT = "https://s3.amazonaws.com";
	var DEFAULT_REGION = "us-east-1";
	var EMPTY_SHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855";

	var root = document.querySelector("[data-vault-app]");
	if (!root) return;

	var elements = {
		vaultList: document.getElementById("vault-list"),
		vaultCount: document.getElementById("vault-count"),
		bucketPanel: document.getElementById("bucket-panel"),
		bucketList: document.getElementById("bucket-list"),
		bucketCount: document.getElementById("bucket-count"),
		welcome: document.getElementById("welcome-state"),
		vaultForm: document.getElementById("vault-form"),
		vaultFormTitle: document.getElementById("vault-form-title"),
		vaultFormHint: document.getElementById("vault-form-hint"),
		deleteVault: document.getElementById("delete-vault"),
		cancelVault: document.getElementById("cancel-vault"),
		addVault: document.getElementById("add-vault"),
		emptyAddVault: document.getElementById("empty-add-vault"),
		importVaults: document.getElementById("import-vaults"),
		exportVaults: document.getElementById("export-vaults"),
		importFile: document.getElementById("import-file"),
		testConnection: document.getElementById("test-connection"),
		modeDirect: document.getElementById("vault-mode-direct"),
		modeProxy: document.getElementById("vault-mode-proxy"),
		proxyModeMessage: document.getElementById("proxy-mode-message"),
		connectionState: document.getElementById("connection-state"),
		connectionMessage: document.getElementById("connection-message"),
		knownVaultBucket: document.getElementById("known-vault-bucket"),
		availableBucketsField: document.getElementById("available-buckets-field"),
		availableBucketsLabel: document.getElementById("available-buckets-label"),
		availableBuckets: document.getElementById("available-buckets"),
		saveVault: document.getElementById("save-vault"),
		addBucket: document.getElementById("add-bucket"),
		bucketForm: document.getElementById("bucket-form"),
		bucketChoice: document.getElementById("bucket-choice"),
		bucketOptions: document.getElementById("bucket-options"),
		masterText: document.getElementById("master-text"),
		masterFile: document.getElementById("master-file"),
		masterFileName: document.getElementById("master-file-name"),
		verifyBucket: document.getElementById("verify-bucket"),
		bucketState: document.getElementById("bucket-state"),
		bucketMessage: document.getElementById("bucket-message"),
		bucketFormHint: document.getElementById("bucket-form-hint"),
		cancelBucket: document.getElementById("cancel-bucket"),
		saveBucket: document.getElementById("save-bucket"),
		fileManager: document.getElementById("file-manager"),
		fileManagerTitle: document.getElementById("file-manager-title"),
		fileManagerSubtitle: document.getElementById("file-manager-subtitle"),
		refreshFiles: document.getElementById("refresh-files"),
		openModeDirect: document.getElementById("open-mode-direct"),
		openModeProxy: document.getElementById("open-mode-proxy"),
		uploadFiles: document.getElementById("upload-files"),
		deleteFiles: document.getElementById("delete-files"),
		uploadInput: document.getElementById("upload-input"),
		editVault: document.getElementById("edit-vault"),
		breadcrumbs: document.getElementById("breadcrumbs"),
		fileList: document.getElementById("file-list"),
		fileStatus: document.getElementById("file-manager-status"),
		viewer: document.getElementById("viewer-dialog"),
		viewerTitle: document.getElementById("viewer-title"),
		viewerStatus: document.getElementById("viewer-status"),
		viewerImage: document.getElementById("viewer-image"),
		viewerVideo: document.getElementById("viewer-video"),
		viewerMessage: document.getElementById("viewer-message"),
		closeViewer: document.getElementById("close-viewer"),
		toast: document.getElementById("vault-toast"),
		fields: {
			name: document.getElementById("vault-name"),
			endpoint: document.getElementById("vault-endpoint"),
			region: document.getElementById("vault-region"),
			accessKeyId: document.getElementById("vault-access-key"),
			secretAccessKey: document.getElementById("vault-secret-key"),
			sessionToken: document.getElementById("vault-session-token"),
			proxyAccessToken: document.getElementById("vault-proxy-token"),
			pathStyle: document.getElementById("vault-path-style"),
			transportMode: document.querySelectorAll('input[name="transportMode"]')
		}
	};

	var state = {
		vaults: [],
		activeVaultId: null,
		activeBucketId: null,
		view: "welcome",
		draft: null,
		bucketDraft: null,
		testedFingerprint: "",
		testedBuckets: [],
		bucketChoices: [],
		bucketVerified: null,
		filePrefix: "",
		db: null,
		toastTimer: null,
		fileRequest: 0,
		proxyAvailable: false,
		permissions: { list: false, read: false, upload: false, delete: false },
		visibleItems: [],
		cryptoCache: new Map(),
		viewerURL: "",
		mediaSource: null,
		viewerAbortController: null
	};

	function requestResult(request) {
		return new Promise(function (resolve, reject) {
			request.onsuccess = function () { resolve(request.result); };
			request.onerror = function () { reject(request.error || new Error("IndexedDB request failed")); };
		});
	}

	function transactionDone(transaction) {
		return new Promise(function (resolve, reject) {
			transaction.oncomplete = function () { resolve(); };
			transaction.onerror = function () { reject(transaction.error || new Error("IndexedDB transaction failed")); };
			transaction.onabort = function () { reject(transaction.error || new Error("IndexedDB transaction aborted")); };
		});
	}

	function openDatabase() {
		return new Promise(function (resolve, reject) {
			if (!window.indexedDB) {
				reject(new Error("IndexedDB is not available in this browser."));
				return;
			}
			var request = window.indexedDB.open(DB_NAME, DB_VERSION);
			request.onupgradeneeded = function () {
				var database = request.result;
				var transaction = request.transaction;
				var vaultStore = database.objectStoreNames.contains(STORE_NAME)
					? transaction.objectStore(STORE_NAME)
					: database.createObjectStore(STORE_NAME, { keyPath: "id" });
				if (!database.objectStoreNames.contains(LEGACY_STORE_NAME)) return;
				var legacyStore = transaction.objectStore(LEGACY_STORE_NAME);
				legacyStore.openCursor().onsuccess = function (event) {
					var cursor = event.target.result;
					if (!cursor) return;
					vaultStore.put(migrateLegacyVault(cursor.value));
					cursor.continue();
				};
			};
			request.onsuccess = function () {
				var database = request.result;
				database.onversionchange = function () { database.close(); };
				resolve(database);
			};
			request.onerror = function () { reject(request.error || new Error("Could not open local vault storage.")); };
		});
	}

	function readVaults() {
		var transaction = state.db.transaction(STORE_NAME, "readonly");
		return requestResult(transaction.objectStore(STORE_NAME).getAll()).then(function (vaults) {
		return vaults.map(function (vault) { return normalizeVault(vault, false); }).sort(function (a, b) { return a.createdAt - b.createdAt; });
		});
	}

	function saveVault(vault) {
		var transaction = state.db.transaction(STORE_NAME, "readwrite");
		transaction.objectStore(STORE_NAME).put(vault);
		return transactionDone(transaction);
	}

	function saveVaults(vaults) {
		var transaction = state.db.transaction(STORE_NAME, "readwrite");
		var store = transaction.objectStore(STORE_NAME);
		vaults.forEach(function (vault) { store.put(vault); });
		return transactionDone(transaction);
	}

	function removeVault(id) {
		var transaction = state.db.transaction(STORE_NAME, "readwrite");
		transaction.objectStore(STORE_NAME).delete(id);
		return transactionDone(transaction);
	}

	function makeID(prefix) {
		if (window.crypto && typeof window.crypto.randomUUID === "function") return (prefix || "id") + "_" + window.crypto.randomUUID();
		var bytes = new Uint8Array(16);
		if (window.crypto && typeof window.crypto.getRandomValues === "function") {
			window.crypto.getRandomValues(bytes);
		} else {
			for (var i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256);
		}
		return (prefix || "id") + "_" + Array.prototype.map.call(bytes, function (byte) {
			return byte.toString(16).padStart(2, "0");
		}).join("");
	}

	function avatarFor(id) {
		var hash = 2166136261;
		for (var i = 0; i < id.length; i++) {
			hash ^= id.charCodeAt(i);
			hash = Math.imul(hash, 16777619);
		}
		return {
			letter: String.fromCharCode(65 + ((hash >>> 0) % 26)),
			color: (hash >>> 5) % 8
		};
	}

	function newVault() {
		var now = Date.now();
		return {
			id: makeID("vault"),
			name: "",
			endpoint: DEFAULT_ENDPOINT,
			region: DEFAULT_REGION,
			accessKeyId: "",
			secretAccessKey: "",
			sessionToken: "",
			proxyAccessToken: "",
			pathStyle: false,
			transportMode: "direct",
			buckets: [],
			createdAt: now,
			updatedAt: now
		};
	}

	function newBucket(name, master, verifiedAt, transportMode) {
		return {
			id: makeID("bucket"),
			name: name,
			master: master,
			transportMode: transportMode === "proxy" ? "proxy" : "direct",
			verifiedAt: verifiedAt || Date.now(),
			updatedAt: Date.now()
		};
	}

	function stringValue(value) {
		return typeof value === "string" ? value : "";
	}

	function secretLabel(id) {
		if (id === "vault-secret-key") return "secret access key";
		if (id === "vault-session-token") return "session token";
		return "server proxy access token";
	}

	function normalizeMaster(master) {
		if (!master || typeof master !== "object") return null;
		if (master.encoding !== "base64" || !stringValue(master.data)) return null;
		return {
			encoding: "base64",
			data: stringValue(master.data),
			filename: stringValue(master.filename) || "master.txt",
			mimeType: stringValue(master.mimeType) || "text/plain",
			size: Number.isFinite(master.size) ? master.size : 0
		};
	}

	function normalizeBucket(bucket, freshID) {
		if (!bucket || typeof bucket !== "object") return null;
		var name = stringValue(bucket.name).trim();
		if (!name) return null;
		return {
			id: freshID ? makeID("bucket") : stringValue(bucket.id) || makeID("bucket"),
			name: name,
			master: normalizeMaster(bucket.master),
			transportMode: bucket.transportMode === "proxy" ? "proxy" : "direct",
			verifiedAt: Number.isFinite(bucket.verifiedAt) ? bucket.verifiedAt : 0,
			updatedAt: Number.isFinite(bucket.updatedAt) ? bucket.updatedAt : Date.now()
		};
	}

	function normalizeVault(raw, freshID) {
		raw = raw || {};
		var buckets = Array.isArray(raw.buckets) ? raw.buckets.map(function (bucket) { return normalizeBucket(bucket, freshID); }).filter(Boolean) : [];
		return {
			id: freshID ? makeID("vault") : stringValue(raw.id) || makeID("vault"),
			name: stringValue(raw.name).trim(),
			endpoint: stringValue(raw.endpoint).trim(),
			region: stringValue(raw.region).trim(),
			accessKeyId: stringValue(raw.accessKeyId).trim(),
			secretAccessKey: stringValue(raw.secretAccessKey),
			sessionToken: stringValue(raw.sessionToken),
			proxyAccessToken: stringValue(raw.proxyAccessToken),
			pathStyle: raw.pathStyle === true,
			transportMode: raw.transportMode === "proxy" ? "proxy" : "direct",
			buckets: buckets,
			createdAt: Number.isFinite(raw.createdAt) ? raw.createdAt : Date.now(),
			updatedAt: Number.isFinite(raw.updatedAt) ? raw.updatedAt : Date.now()
		};
	}

	function migrateLegacyVault(raw) {
		var vault = normalizeVault(raw, false);
		if (!vault.endpoint) vault.endpoint = DEFAULT_ENDPOINT;
		if (!vault.region) vault.region = DEFAULT_REGION;
		return vault;
	}

	function accountByID(id) {
		return state.vaults.find(function (vault) { return vault.id === id; }) || null;
	}

	function bucketByID(vault, id) {
		return vault && vault.buckets.find(function (bucket) { return bucket.id === id; }) || null;
	}

	function bucketByName(vault, name) {
		return vault && vault.buckets.find(function (bucket) { return bucket.name === name; }) || null;
	}

	function validEndpoint(endpoint) {
		try {
			var parsed = new URL(endpoint);
			return parsed.protocol === "https:" || parsed.protocol === "http:";
		} catch (_) {
			return false;
		}
	}

	function bucketName(value) {
		return stringValue(value).trim();
	}

	function validBucketName(value) {
		return /^[A-Za-z0-9._-]{1,255}$/.test(value);
	}

	function vaultFingerprint(vault) {
		return [vault.endpoint, vault.region, vault.accessKeyId, vault.secretAccessKey, vault.sessionToken, vault.proxyAccessToken, vault.pathStyle ? "path" : "virtual", vault.transportMode || "direct"].join("\u001f");
	}

	function collectVaultForm() {
		var fields = elements.fields;
		return {
			name: fields.name.value.trim(),
			endpoint: fields.endpoint.value.trim().replace(/\/$/, ""),
			region: fields.region.value.trim(),
			accessKeyId: fields.accessKeyId.value.trim(),
			secretAccessKey: fields.secretAccessKey.value,
			sessionToken: fields.sessionToken.value,
			proxyAccessToken: fields.proxyAccessToken.value,
			pathStyle: fields.pathStyle.checked,
			transportMode: Array.prototype.find.call(fields.transportMode, function (input) { return input.checked; }).value
		};
	}

	function requiredVaultFields(vault) {
		var labels = {
			name: "vault name",
			endpoint: "S3 endpoint",
			region: "region",
			accessKeyId: "access key ID",
			secretAccessKey: "secret access key"
		};
		return Object.keys(labels).filter(function (key) {
			return !stringValue(vault[key]).trim();
		}).map(function (key) { return labels[key]; });
	}

	function editorChanged() {
		if (state.view !== "vault-form") return false;
		var original = state.draft || accountByID(state.activeVaultId);
		if (!original) return false;
		var current = collectVaultForm();
		return ["name", "endpoint", "region", "accessKeyId", "secretAccessKey", "sessionToken", "proxyAccessToken", "pathStyle", "transportMode"].some(function (key) {
			return current[key] !== original[key];
		});
	}

	function confirmEditorExit() {
		return !editorChanged() || window.confirm("Discard unsaved vault changes?");
	}

	function fillVaultForm(vault) {
		var fields = elements.fields;
		fields.name.value = vault.name || "";
		fields.endpoint.value = vault.endpoint || DEFAULT_ENDPOINT;
		fields.region.value = vault.region || DEFAULT_REGION;
		fields.accessKeyId.value = vault.accessKeyId || "";
		fields.secretAccessKey.value = vault.secretAccessKey || "";
		fields.sessionToken.value = vault.sessionToken || "";
		fields.proxyAccessToken.value = vault.proxyAccessToken || "";
		elements.knownVaultBucket.value = "";
		fields.pathStyle.checked = vault.pathStyle === true;
		Array.prototype.forEach.call(fields.transportMode, function (input) { input.checked = input.value === (vault.transportMode || "direct"); });
		[fields.secretAccessKey, fields.sessionToken, fields.proxyAccessToken].forEach(function (field) {
			field.type = "password";
			var button = root.querySelector('[data-reveal="' + field.id + '"]');
			if (button) {
				button.textContent = "Show";
				button.setAttribute("aria-label", "Show " + secretLabel(field.id));
			}
		});
	}

	function clearConnectionTest() {
		state.testedFingerprint = "";
		state.testedBuckets = [];
		elements.saveVault.hidden = true;
		elements.availableBucketsField.hidden = true;
		elements.availableBucketsLabel.textContent = "Discovered buckets";
		elements.connectionState.textContent = "Not tested";
		elements.connectionState.dataset.state = "idle";
		elements.connectionMessage.textContent = "Test a known bucket, or leave it blank to list the buckets visible to these credentials. A successful test unlocks saving this vault.";
	}

	function selectedTransportMode() {
		var selected = Array.prototype.find.call(elements.fields.transportMode, function (input) { return input.checked; });
		return selected ? selected.value : "direct";
	}

	function updateTransportModeUI() {
		var proxyBlocked = !state.proxyAvailable;
		var vault = accountByID(state.activeVaultId);
		var bucket = bucketByID(vault, state.activeBucketId);
		var mode = bucket ? activeTransportMode(vault, bucket.name) : selectedTransportMode();
		elements.modeProxy.disabled = proxyBlocked;
		elements.openModeProxy.disabled = proxyBlocked;
		elements.proxyModeMessage.textContent = state.proxyAvailable
			? "Choose a default for verification and new buckets. Each registered bucket can switch independently."
			: "Server proxy is not enabled on this deployment. Direct mode requires provider CORS.";
		elements.openModeDirect.classList.toggle("is-active", mode === "direct");
		elements.openModeProxy.classList.toggle("is-active", mode === "proxy");
		elements.openModeDirect.setAttribute("aria-pressed", String(mode === "direct"));
		elements.openModeProxy.setAttribute("aria-pressed", String(mode === "proxy"));
	}

	function setTransportMode(mode, persistDefault) {
		if (mode === "proxy" && !state.proxyAvailable) {
			showToast("Server proxy is not available on this deployment.", "error");
			return;
		}
		if (persistDefault) {
			Array.prototype.forEach.call(elements.fields.transportMode, function (input) { input.checked = input.value === mode; });
			clearConnectionTest();
			updateTransportModeUI();
			return;
		}
		var vault = accountByID(state.activeVaultId);
		var bucket = bucketByID(vault, state.activeBucketId);
		if (!vault || !bucket) return;
		if (mode === "proxy" && !vault.proxyAccessToken) {
			showToast("Edit this vault to add the server proxy access token.", "error");
			return;
		}
		if (bucket.transportMode === mode) return;
		var previousMode = bucket.transportMode;
		bucket.transportMode = mode;
		bucket.updatedAt = Date.now();
		vault.updatedAt = Date.now();
		updateTransportModeUI();
		renderBucketList();
		loadFileList();
		saveVault(vault).then(function () {
			showToast((mode === "proxy" ? "Proxy" : "Direct") + " mode saved for “" + bucket.name + "”.", "success");
		}).catch(function () {
			bucket.transportMode = previousMode;
			updateTransportModeUI();
			renderBucketList();
			loadFileList();
			showToast("Could not save this bucket's connection mode.", "error");
		});
	}

	function activeTransportMode(vault, bucketName) {
		var bucket = bucketName ? bucketByName(vault, bucketName) : bucketByID(vault, state.activeBucketId);
		var mode = bucket ? bucket.transportMode : (vault && vault.transportMode) || "direct";
		return mode === "proxy" && !state.proxyAvailable ? "direct" : mode;
	}

	function checkProxyAvailability() {
		return fetch("/vault/proxy/status", { credentials: "same-origin", cache: "no-store" }).then(function (response) {
			state.proxyAvailable = response.ok;
		}).catch(function () {
			state.proxyAvailable = false;
		}).finally(updateTransportModeUI);
	}

	function showToast(message, kind) {
		window.clearTimeout(state.toastTimer);
		elements.toast.textContent = message;
		elements.toast.dataset.kind = kind || "";
		elements.toast.hidden = false;
		state.toastTimer = window.setTimeout(function () { elements.toast.hidden = true; }, 5000);
	}

	function makeAvatar(vault, className) {
		var avatar = document.createElement("span");
		var visual = avatarFor(vault.id);
		avatar.className = className || "account-avatar";
		avatar.dataset.color = String(visual.color);
		avatar.textContent = visual.letter;
		avatar.setAttribute("aria-hidden", "true");
		return avatar;
	}

	function renderVaultList() {
		elements.vaultList.replaceChildren();
		state.vaults.forEach(function (vault) {
			var button = document.createElement("button");
			button.type = "button";
			button.className = "account-row" + (vault.id === state.activeVaultId ? " is-active" : "");
			button.dataset.vaultId = vault.id;
			button.appendChild(makeAvatar(vault));
			var copy = document.createElement("span");
			copy.className = "account-row-copy";
			var name = document.createElement("strong");
			name.textContent = vault.name || "Unnamed vault";
			var detail = document.createElement("small");
			detail.textContent = vault.buckets.length + " bucket" + (vault.buckets.length === 1 ? "" : "s");
			copy.appendChild(name);
			copy.appendChild(detail);
			button.appendChild(copy);
			elements.vaultList.appendChild(button);
		});
	}

	function renderBucketList() {
		var vault = accountByID(state.activeVaultId);
		elements.bucketPanel.hidden = !vault || state.view === "vault-form";
		elements.bucketList.replaceChildren();
		if (!vault) {
			elements.bucketCount.textContent = "0 configured";
			return;
		}
		elements.bucketCount.textContent = vault.buckets.length + " configured";
		vault.buckets.forEach(function (bucket) {
			var button = document.createElement("button");
			button.type = "button";
			button.className = "bucket-row" + (bucket.id === state.activeBucketId ? " is-active" : "");
			button.dataset.bucketId = bucket.id;
			var icon = document.createElement("span");
			icon.className = "bucket-icon";
			icon.textContent = "▰";
			icon.setAttribute("aria-hidden", "true");
			var copy = document.createElement("span");
			copy.className = "account-row-copy";
			var name = document.createElement("strong");
			name.textContent = bucket.name;
			var detail = document.createElement("small");
			detail.textContent = (bucket.master ? "Master file saved" : "Master file missing") + " · " + (bucket.transportMode === "proxy" ? "Proxy" : "Direct");
			copy.appendChild(name);
			copy.appendChild(detail);
			button.appendChild(icon);
			button.appendChild(copy);
			elements.bucketList.appendChild(button);
		});
	}

	function render() {
		var hasVaults = state.vaults.length > 0;
		elements.vaultCount.textContent = state.vaults.length + " saved";
		elements.welcome.hidden = state.view !== "welcome";
		elements.vaultForm.hidden = state.view !== "vault-form";
		elements.bucketForm.hidden = state.view !== "bucket-form";
		elements.fileManager.hidden = state.view !== "files";
		elements.deleteVault.hidden = state.draft !== null || !state.activeVaultId;
		renderVaultList();
		renderBucketList();
		updateTransportModeUI();
		updatePermissionActions();
		if (!hasVaults && state.view === "files") state.view = "welcome";
	}

	function startNewVault() {
		if (!confirmEditorExit()) return;
		state.activeVaultId = null;
		state.activeBucketId = null;
		state.draft = newVault();
		state.view = "vault-form";
		fillVaultForm(state.draft);
		clearConnectionTest();
		elements.vaultFormTitle.textContent = "New vault";
		elements.vaultFormHint.textContent = "Required fields are marked with an asterisk.";
		render();
		elements.fields.name.focus();
	}

	function editActiveVault() {
		var vault = accountByID(state.activeVaultId);
		if (!vault || !confirmEditorExit()) return;
		state.draft = null;
		state.view = "vault-form";
		fillVaultForm(vault);
		clearConnectionTest();
		elements.vaultFormTitle.textContent = vault.name || "Edit vault";
		elements.vaultFormHint.textContent = "Test the updated credentials before saving changes.";
		render();
	}

	function openVault(id) {
		if (id === state.activeVaultId && state.view === "files") return;
		if (!confirmEditorExit()) return;
		var vault = accountByID(id);
		if (!vault) return;
		state.activeVaultId = id;
		state.activeBucketId = vault.buckets.length ? vault.buckets[0].id : null;
		state.draft = null;
		state.view = "files";
		state.filePrefix = "";
		render();
		loadFileList();
	}

	function cancelVault() {
		if (!confirmEditorExit()) return;
		if (state.vaults.length > 0) {
			state.draft = null;
			state.view = "files";
			if (!state.activeVaultId) state.activeVaultId = state.vaults[0].id;
			var vault = accountByID(state.activeVaultId);
			state.activeBucketId = vault && vault.buckets.length ? vault.buckets[0].id : null;
			render();
			loadFileList();
		} else {
			state.draft = null;
			state.view = "welcome";
			render();
		}
	}

	function deleteActiveVault() {
		var vault = accountByID(state.activeVaultId);
		if (!vault || !window.confirm("Delete “" + (vault.name || "this vault") + "” and its local bucket registrations?")) return;
		removeVault(vault.id).then(function () {
			state.vaults = state.vaults.filter(function (item) { return item.id !== vault.id; });
			state.activeVaultId = state.vaults.length ? state.vaults[0].id : null;
			state.activeBucketId = null;
			state.view = state.activeVaultId ? "files" : "welcome";
			render();
			if (state.activeVaultId) loadFileList();
			showToast("Vault removed from this browser.", "success");
		}).catch(function () { showToast("Could not remove this vault.", "error"); });
	}

	function setConnectionState(stateName, message) {
		elements.connectionState.textContent = stateName;
		elements.connectionState.dataset.state = stateName.toLowerCase().replace(/\s+/g, "-");
		elements.connectionMessage.textContent = message;
	}

	function populateBucketSelect(select, buckets, emptyLabel) {
		select.replaceChildren();
		if (select.tagName === "DATALIST") {
			buckets.forEach(function (bucket) {
				var suggestion = document.createElement("option");
				suggestion.value = bucket.name;
				select.appendChild(suggestion);
			});
			return;
		}
		if (!buckets.length) {
			var empty = document.createElement("option");
			empty.value = "";
			empty.textContent = emptyLabel || "No buckets discovered";
			select.appendChild(empty);
			return;
		}
		buckets.forEach(function (bucket) {
			var option = document.createElement("option");
			option.value = bucket.name;
			option.textContent = bucket.name;
			select.appendChild(option);
		});
	}

	function testConnection() {
		var draft = collectVaultForm();
		var knownBucket = bucketName(elements.knownVaultBucket.value);
		var missing = requiredVaultFields(draft);
		if (missing.length) {
			elements.vaultFormHint.textContent = "Please fill in: " + missing.join(", ") + ".";
			showToast("Some required fields are missing.", "error");
			return;
		}
		if (!validEndpoint(draft.endpoint)) {
			elements.vaultFormHint.textContent = "The endpoint must be a valid http:// or https:// URL.";
			showToast("Check the S3 endpoint URL.", "error");
			return;
		}
		if (knownBucket && !validBucketName(knownBucket)) {
			elements.vaultFormHint.textContent = "Known bucket names may contain letters, numbers, dots, hyphens, and underscores only.";
			showToast("Enter a valid bucket name.", "error");
			return;
		}
		if (draft.transportMode === "proxy" && new URL(draft.endpoint).protocol !== "https:") {
			elements.vaultFormHint.textContent = "Server proxy mode requires an HTTPS S3 endpoint.";
			showToast("Proxy mode requires HTTPS.", "error");
			return;
		}
		if (draft.transportMode === "proxy" && !draft.proxyAccessToken) {
			elements.vaultFormHint.textContent = "Enter the server proxy access token before testing proxy mode.";
			showToast("Proxy mode requires the server access token.", "error");
			return;
		}
		var fingerprint = vaultFingerprint(draft);
		state.testedFingerprint = "";
		state.testedBuckets = [];
		elements.testConnection.disabled = true;
		elements.saveVault.hidden = true;
		elements.availableBucketsField.hidden = true;
		if (draft.transportMode === "proxy" && !state.proxyAvailable) {
			elements.testConnection.disabled = false;
			setConnectionState("Unavailable", "Server proxy mode is not enabled on this deployment.");
			return;
		}
		var verification = knownBucket
			? listBucket(draft, knownBucket, "", 1, false).then(function () { return [{ name: knownBucket }]; })
			: listBuckets(draft);
		setConnectionState("Testing…", knownBucket
			? (draft.transportMode === "proxy" ? "Verifying the known bucket through the isolated server proxy." : "Verifying the known bucket directly from this browser.")
			: (draft.transportMode === "proxy" ? "Sending credentials to the isolated server proxy for a bucket-list request." : "Contacting the S3 endpoint directly from this browser and requesting its bucket list."));
		verification.then(function (buckets) {
			state.testedFingerprint = fingerprint;
			state.testedBuckets = buckets;
			populateBucketSelect(elements.availableBuckets, buckets, "No buckets visible to these credentials");
			elements.availableBucketsLabel.textContent = knownBucket ? "Verified bucket" : "Discovered buckets";
			elements.availableBucketsField.hidden = false;
			elements.saveVault.hidden = false;
			setConnectionState("Verified", knownBucket
				? "Known bucket verified. You can now save this vault without a master file."
				: buckets.length + " bucket" + (buckets.length === 1 ? "" : "s") + " listed successfully. You can now save this vault without a master file.");
		}).catch(function (error) {
			var message = error.message;
			if (!knownBucket && draft.transportMode === "direct") message += " If your provider uses bucket-scoped CORS, enter a known bucket above and test it directly.";
			setConnectionState("Failed", message);
			showToast("Connection test failed.", "error");
		}).finally(function () {
			elements.testConnection.disabled = false;
		});
	}

	function saveCurrentVault(event) {
		event.preventDefault();
		var values = collectVaultForm();
		var missing = requiredVaultFields(values);
		if (missing.length) {
			elements.vaultFormHint.textContent = "Please fill in: " + missing.join(", ") + ".";
			return;
		}
		if (vaultFingerprint(values) !== state.testedFingerprint) {
			elements.vaultFormHint.textContent = "Test the current credentials before saving this vault.";
			showToast("The vault needs a successful connection test.", "error");
			return;
		}
		var existing = state.activeVaultId ? accountByID(state.activeVaultId) : null;
		var vault = {
			id: existing ? existing.id : state.draft.id,
			name: values.name,
			endpoint: values.endpoint,
			region: values.region,
			accessKeyId: values.accessKeyId,
			secretAccessKey: values.secretAccessKey,
			sessionToken: values.sessionToken,
			proxyAccessToken: values.proxyAccessToken,
			pathStyle: values.pathStyle,
			transportMode: values.transportMode,
			buckets: existing ? existing.buckets : [],
			createdAt: existing ? existing.createdAt : Date.now(),
			updatedAt: Date.now()
		};
		saveVault(vault).then(function () {
			state.vaults = state.vaults.filter(function (item) { return item.id !== vault.id; });
			state.vaults.push(vault);
			state.vaults.sort(function (a, b) { return a.createdAt - b.createdAt; });
			state.activeVaultId = vault.id;
			state.activeBucketId = vault.buckets.length ? vault.buckets[0].id : null;
			state.draft = null;
			state.view = "files";
			render();
			loadFileList();
			showToast(existing ? "Vault updated locally." : "Vault saved locally.", "success");
		}).catch(function () { showToast("Could not save this vault in the browser.", "error"); });
	}

	function startBucketSetup() {
		var vault = accountByID(state.activeVaultId);
		if (!vault || !confirmEditorExit()) return;
		state.bucketDraft = null;
		state.bucketChoices = state.testedBuckets.slice();
		state.bucketVerified = null;
		state.view = "bucket-form";
		elements.bucketState.textContent = "Not verified";
		elements.bucketState.dataset.state = "idle";
		elements.bucketMessage.textContent = "Type a bucket name or choose one discovered during connection verification, then verify access with the tested credentials.";
		elements.bucketFormHint.textContent = "A master text or file is required before saving the bucket.";
		elements.masterText.value = "";
		elements.masterFile.value = "";
		elements.masterFileName.textContent = "No file selected.";
		elements.saveBucket.hidden = true;
		populateBucketSelect(elements.bucketOptions, state.bucketChoices);
		elements.bucketChoice.value = state.bucketChoices.length === 1 ? state.bucketChoices[0].name : "";
		render();
		listBuckets(vault).then(function (buckets) {
			state.bucketChoices = buckets;
			populateBucketSelect(elements.bucketOptions, buckets);
			if (!buckets.length) elements.bucketMessage.textContent = "No buckets were returned for this vault.";
		}).catch(function (error) {
			elements.bucketMessage.textContent = "Automatic discovery failed. Type a bucket name directly to verify it. " + error.message;
		});
	}

	function masterChanged() {
		state.bucketVerified = null;
		elements.saveBucket.hidden = true;
		elements.bucketState.textContent = "Not verified";
		elements.bucketState.dataset.state = "idle";
	}

	function readMaster() {
		var file = elements.masterFile.files && elements.masterFile.files[0];
		if (file) {
			if (file.size > MAX_MASTER_BYTES) return Promise.reject(new Error("Master files are limited to 8 MB."));
			return file.arrayBuffer().then(function (buffer) {
				return {
					encoding: "base64",
					data: bytesToBase64(new Uint8Array(buffer)),
					filename: file.name || "master",
					mimeType: file.type || "application/octet-stream",
					size: file.size
				};
			});
		}
		var text = elements.masterText.value;
		if (!text) return Promise.reject(new Error("Enter master text or choose a master file."));
		var bytes = new TextEncoder().encode(text);
		if (bytes.byteLength > MAX_MASTER_BYTES) return Promise.reject(new Error("Master text is limited to 8 MB."));
		return Promise.resolve({
			encoding: "base64",
			data: bytesToBase64(bytes),
			filename: "master.txt",
			mimeType: "text/plain;charset=utf-8",
			size: bytes.byteLength
		});
	}

	function verifyBucket() {
		var vault = accountByID(state.activeVaultId);
		var bucket = bucketName(elements.bucketChoice.value);
		if (!vault || !bucket) {
			showToast("Enter or choose a bucket first.", "error");
			return;
		}
		if (!validBucketName(bucket)) {
			elements.bucketMessage.textContent = "Bucket names may contain letters, numbers, dots, hyphens, and underscores only.";
			showToast("Enter a valid bucket name.", "error");
			return;
		}
		if (!elements.masterText.value && !(elements.masterFile.files && elements.masterFile.files[0])) {
			elements.bucketState.textContent = "Master required";
			elements.bucketState.dataset.state = "failed";
			elements.bucketMessage.textContent = "Add master text or choose a master file before verifying this bucket.";
			return;
		}
		elements.verifyBucket.disabled = true;
		elements.saveBucket.hidden = true;
		elements.bucketState.textContent = "Testing…";
		elements.bucketState.dataset.state = "testing";
		elements.bucketMessage.textContent = activeTransportMode(vault, bucket) === "proxy"
			? "Requesting the bucket root through the isolated server proxy."
			: "Requesting the bucket root directly from this browser.";
		var accessVerified = false;
		listBucket(vault, bucket, "", 1, false).then(function () {
			accessVerified = true;
			state.bucketVerified = { vault: vaultFingerprint(vault), name: bucket };
			elements.bucketState.textContent = "Verified";
			elements.bucketState.dataset.state = "verified";
			elements.bucketMessage.textContent = "Bucket access verified. Add a master text or file to register it.";
			return readMaster();
		}).then(function () {
			elements.saveBucket.hidden = false;
		}).catch(function (error) {
			if (!accessVerified) {
				state.bucketVerified = null;
				elements.bucketState.textContent = "Failed";
				elements.bucketState.dataset.state = "failed";
				elements.bucketMessage.textContent = error.message;
				showToast("Bucket verification failed.", "error");
			} else {
				elements.bucketState.textContent = "Verified";
				elements.bucketState.dataset.state = "verified";
				elements.bucketMessage.textContent = error.message;
			}
		}).finally(function () { elements.verifyBucket.disabled = false; });
	}

	function saveRegisteredBucket() {
		var vault = accountByID(state.activeVaultId);
		var registeredBucketName = bucketName(elements.bucketChoice.value);
		if (!vault || !registeredBucketName || !state.bucketVerified || state.bucketVerified.name !== registeredBucketName || state.bucketVerified.vault !== vaultFingerprint(vault)) {
			showToast("Verify the selected bucket before saving it.", "error");
			return;
		}
		readMaster().then(function (master) {
			var existing = vault.buckets.find(function (bucket) { return bucket.name === registeredBucketName; });
			var bucket = newBucket(registeredBucketName, master, Date.now(), existing ? existing.transportMode : vault.transportMode);
			if (existing) bucket.id = existing.id;
			vault.buckets = vault.buckets.filter(function (item) { return item.name !== registeredBucketName; });
			vault.buckets.push(bucket);
			vault.updatedAt = Date.now();
			return saveVault(vault).then(function () { return bucket; });
		}).then(function (bucket) {
			state.activeBucketId = bucket.id;
			state.view = "files";
			state.filePrefix = "";
			render();
			loadFileList();
			showToast("Bucket registered locally.", "success");
		}).catch(function (error) {
			elements.bucketFormHint.textContent = error.message;
			showToast("Could not register the bucket.", "error");
		});
	}

	function cancelBucket() {
		state.bucketDraft = null;
		state.view = "files";
		render();
		loadFileList();
	}

	function openBucket(id) {
		var vault = accountByID(state.activeVaultId);
		if (!vault || !bucketByID(vault, id)) return;
		state.activeBucketId = id;
		state.view = "files";
		state.filePrefix = "";
		render();
		loadFileList();
	}

	function bytesToBase64(bytes) {
		var binary = "";
		var chunkSize = 0x8000;
		for (var i = 0; i < bytes.length; i += chunkSize) {
			binary += String.fromCharCode.apply(null, bytes.subarray(i, i + chunkSize));
		}
		return btoa(binary);
	}

	function base64ToBytes(value) {
		var binary = atob(value.replace(/\s+/g, ""));
		var bytes = new Uint8Array(binary.length);
		for (var i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
		return bytes;
	}

	function bytesToBase64URL(bytes) {
		return bytesToBase64(bytes).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
	}

	function base64URLToBytes(value) {
		var padded = value.replace(/-/g, "+").replace(/_/g, "/");
		while (padded.length % 4) padded += "=";
		return base64ToBytes(padded);
	}

	function utf8(value) { return new TextEncoder().encode(value); }

	function bytesToHex(bytes) {
		return Array.prototype.map.call(new Uint8Array(bytes), function (byte) { return byte.toString(16).padStart(2, "0"); }).join("");
	}

	function sha256(value) {
		return crypto.subtle.digest("SHA-256", value instanceof Uint8Array ? value : utf8(value)).then(bytesToHex);
	}

	function hmac(key, value) {
		return crypto.subtle.importKey("raw", key, { name: "HMAC", hash: "SHA-256" }, false, ["sign"]).then(function (cryptoKey) {
			return crypto.subtle.sign("HMAC", cryptoKey, utf8(value));
		}).then(function (signature) { return new Uint8Array(signature); });
	}

	function hmacBytes(key, data) {
		return crypto.subtle.importKey("raw", key, { name: "HMAC", hash: "SHA-256" }, false, ["sign"]).then(function (cryptoKey) {
			return crypto.subtle.sign("HMAC", cryptoKey, data);
		}).then(function (signature) { return new Uint8Array(signature); });
	}

	function hkdfBytes(material, salt, info) {
		return crypto.subtle.importKey("raw", material, "HKDF", false, ["deriveBits"]).then(function (cryptoKey) {
			return crypto.subtle.deriveBits({ name: "HKDF", hash: "SHA-256", salt: salt, info: info }, cryptoKey, 256);
		}).then(function (bits) { return new Uint8Array(bits); });
	}

	async function aesCTRBytes(key, nonce8, position, data) {
		var blockIndex = BigInt(Math.floor(position / 16));
		var counter = new Uint8Array(16);
		counter.set(nonce8, 0);
		for (var i = 7; i >= 0; i--) {
			counter[8 + i] = Number(blockIndex & 255n);
			blockIndex >>= 8n;
		}
		var intraOffset = position % 16;
		var padded = new Uint8Array(intraOffset + data.length);
		padded.set(data, intraOffset);
		var cryptoKey = await crypto.subtle.importKey("raw", key, { name: "AES-CTR" }, false, ["encrypt", "decrypt"]);
		var result = await crypto.subtle.encrypt({ name: "AES-CTR", counter: counter, length: 64 }, cryptoKey, padded);
		return new Uint8Array(result).slice(intraOffset);
	}

	async function bucketCrypto(bucket) {
		var cached = state.cryptoCache.get(bucket.id);
		if (cached) return cached;
		if (!bucket.master || bucket.master.encoding !== "base64" || !bucket.master.data) throw new Error("This bucket has no local master file.");
		var rootKey = base64ToBytes(bucket.master.data);
		var bucketKey = await hkdfBytes(rootKey, utf8(bucket.name), utf8("s3zero-bucket-key"));
		var pathKey = await hkdfBytes(bucketKey, new Uint8Array(0), utf8("s3zero-path-key"));
		var result = { bucketKey: bucketKey, pathKey: pathKey };
		state.cryptoCache.set(bucket.id, result);
		return result;
	}

	async function encryptPathSegment(pathKey, segment) {
		var bytes = utf8(segment);
		var nonce = (await hmacBytes(pathKey, bytes)).slice(0, 8);
		var ciphertext = await aesCTRBytes(pathKey, nonce, 0, bytes);
		var combined = new Uint8Array(nonce.length + ciphertext.length);
		combined.set(nonce, 0);
		combined.set(ciphertext, nonce.length);
		return bytesToBase64URL(combined);
	}

	async function decryptPathSegment(pathKey, token) {
		var data;
		try { data = base64URLToBytes(token); } catch (_) { throw new Error("Invalid encrypted path segment."); }
		if (data.length < 8) throw new Error("Invalid encrypted path segment.");
		var nonce = data.slice(0, 8);
		var plaintext = await aesCTRBytes(pathKey, nonce, 0, data.slice(8));
		var expected = (await hmacBytes(pathKey, plaintext)).slice(0, 8);
		for (var i = 0; i < 8; i++) if (nonce[i] !== expected[i]) throw new Error("Encrypted path integrity check failed.");
		return new TextDecoder().decode(plaintext);
	}

	async function decryptObjectKey(pathKey, encryptedKey) {
		var segments = encryptedKey.split("/");
		var plaintext = [];
		for (var i = 0; i < segments.length; i++) plaintext.push(await decryptPathSegment(pathKey, segments[i]));
		return plaintext.join("/");
	}

	async function encryptPrefix(pathKey, plaintextPrefix) {
		if (!plaintextPrefix) return "";
		var trailing = plaintextPrefix.endsWith("/");
		var trimmed = trailing ? plaintextPrefix.slice(0, -1) : plaintextPrefix;
		if (!trimmed) return trailing ? (await encryptPathSegment(pathKey, "")) + "/" : "";
		var tokens = [];
		for (var i = 0; i < trimmed.split("/").length; i++) tokens.push(await encryptPathSegment(pathKey, trimmed.split("/")[i]));
		return tokens.join("/") + (trailing ? "/" : "");
	}

	function encodeRFC3986(value) {
		return encodeURIComponent(value).replace(/[!'()*]/g, function (char) { return "%" + char.charCodeAt(0).toString(16).toUpperCase(); });
	}

	function canonicalURI(pathname) {
		return (pathname || "/").split("/").map(function (segment) {
			try { return encodeRFC3986(decodeURIComponent(segment)); } catch (_) { return encodeRFC3986(segment); }
		}).join("/") || "/";
	}

	function canonicalQuery(entries) {
		return entries.map(function (entry) { return [encodeRFC3986(entry[0]), encodeRFC3986(entry[1])]; }).sort(function (a, b) {
		return a[0] === b[0] ? a[1].localeCompare(b[1]) : a[0].localeCompare(b[0]);
	}).map(function (entry) { return entry[0] + "=" + entry[1]; }).join("&");
	}

	function amzDate(date) {
		return date.getUTCFullYear().toString() + String(date.getUTCMonth() + 1).padStart(2, "0") + String(date.getUTCDate()).padStart(2, "0") + "T" + String(date.getUTCHours()).padStart(2, "0") + String(date.getUTCMinutes()).padStart(2, "0") + String(date.getUTCSeconds()).padStart(2, "0") + "Z";
	}

	function endpointURL(vault, bucket, entries, objectKey) {
		var url = new URL(vault.endpoint);
		url.hash = "";
		url.search = "";
		if (bucket && !vault.pathStyle) {
			url.hostname = bucket + "." + url.hostname;
		}
		var path = url.pathname.replace(/\/+$/, "");
		if (bucket && vault.pathStyle) path += "/" + encodeRFC3986(bucket);
		if (objectKey) path += "/" + objectKey.split("/").map(encodeRFC3986).join("/");
		url.pathname = objectKey ? path : path + "/";
		url.search = canonicalQuery(entries);
		return url;
	}

	function friendlyS3Error(responseText, status) {
		var code = "";
		var message = "";
		try {
			var xml = new DOMParser().parseFromString(responseText, "application/xml");
			var codeNode = xml.getElementsByTagName("Code")[0];
			var messageNode = xml.getElementsByTagName("Message")[0];
			code = codeNode ? codeNode.textContent : "";
			message = messageNode ? messageNode.textContent : "";
		} catch (_) {}
		return (code || "S3 request failed") + (message ? ": " + message : " (HTTP " + status + ")");
	}

	async function directStorageResponse(vault, bucket, entries, objectKey, range, signal) {
		var url = endpointURL(vault, bucket, entries || [], objectKey);
		var now = new Date();
		var timestamp = amzDate(now);
		var shortDate = timestamp.slice(0, 8);
		var headers = {
			"x-amz-content-sha256": EMPTY_SHA256,
			"x-amz-date": timestamp
		};
		if (vault.sessionToken) headers["x-amz-security-token"] = vault.sessionToken;
		var signedNames = Object.keys(headers).concat(["host"]).sort();
		var canonicalHeaders = signedNames.map(function (name) {
			var value = name === "host" ? url.host : headers[name];
			return name + ":" + String(value).trim().replace(/\s+/g, " ") + "\n";
		}).join("");
		var signedHeaders = signedNames.join(";");
		var canonicalRequest = ["GET", canonicalURI(url.pathname), canonicalQuery(entries || []), canonicalHeaders, signedHeaders, EMPTY_SHA256].join("\n");
		var canonicalRequestHash = await sha256(canonicalRequest);
		var scope = shortDate + "/" + vault.region + "/s3/aws4_request";
		var stringToSign = "AWS4-HMAC-SHA256\n" + timestamp + "\n" + scope + "\n" + canonicalRequestHash;
		var dateKey = await hmac(utf8("AWS4" + vault.secretAccessKey), shortDate);
		var regionKey = await hmac(dateKey, vault.region);
		var serviceKey = await hmac(regionKey, "s3");
		var signingKey = await hmac(serviceKey, "aws4_request");
		var signature = bytesToHex(await hmac(signingKey, stringToSign));
		var authorization = "AWS4-HMAC-SHA256 Credential=" + vault.accessKeyId + "/" + scope + ", SignedHeaders=" + signedHeaders + ", Signature=" + signature;
		var response;
		try {
			var requestHeaders = Object.assign({}, headers, { Authorization: authorization });
			if (range) requestHeaders.Range = range;
			response = await fetch(url.toString(), {
				method: "GET",
				mode: "cors",
				credentials: "omit",
				headers: requestHeaders,
				signal: signal
				});
			} catch (error) {
				if (error.name === "AbortError") throw error;
				throw new Error("The browser could not reach this S3 endpoint. Check HTTPS, endpoint URL, and provider CORS settings.");
		}
		if (!response.ok) {
			var text = await response.text();
			throw new Error(friendlyS3Error(text, response.status));
		}
		return response;
	}

	function proxyPayload(vault, extras) {
		return Object.assign({
			endpoint: vault.endpoint,
			region: vault.region,
			accessKeyId: vault.accessKeyId,
			secretAccessKey: vault.secretAccessKey,
			sessionToken: vault.sessionToken,
			pathStyle: vault.pathStyle === true
		}, extras || {});
	}

	async function proxyStorageResponse(vault, operation, extras, range, signal) {
		var requestHeaders = { "Content-Type": "application/json", "X-Vault-Access-Token": vault.proxyAccessToken || "" };
		var response;
		try {
			response = await fetch("/vault/proxy/" + operation, {
				method: "POST",
				credentials: "same-origin",
				cache: "no-store",
				headers: requestHeaders,
				body: JSON.stringify(proxyPayload(vault, extras)),
				signal: signal
			});
		} catch (error) {
			if (error.name === "AbortError") throw error;
			throw new Error("The isolated server proxy could not be reached.");
		}
		if (!response.ok) {
			var text = await response.text();
			throw new Error(text || "Server proxy request failed (HTTP " + response.status + ").");
		}
		return response;
	}

	async function storageResponse(vault, bucket, entries, objectKey, range, signal) {
		if (activeTransportMode(vault, bucket) === "proxy") {
			if (objectKey) return proxyStorageResponse(vault, "object", { bucket: bucket, key: objectKey, range: range || "" }, "", signal);
			return proxyStorageResponse(vault, bucket ? "list-objects" : "list-buckets", { bucket: bucket || "", query: entries || [] }, range, signal);
		}
		return directStorageResponse(vault, bucket, entries, objectKey, range, signal);
	}

	async function storageText(vault, bucket, entries) {
		var response = await storageResponse(vault, bucket, entries, "", "");
		return response.text();
	}

	function xmlChildren(xml, tag) {
		return Array.prototype.slice.call(xml.getElementsByTagName(tag));
	}

	function xmlValue(parent, tag) {
		var node = parent.getElementsByTagName(tag)[0];
		return node ? node.textContent : "";
	}

	async function listBuckets(vault) {
		var text = await storageText(vault, "", []);
		var xml = new DOMParser().parseFromString(text, "application/xml");
		if (xml.getElementsByTagName("parsererror").length) throw new Error("The S3 endpoint returned invalid XML.");
		return xmlChildren(xml, "Bucket").map(function (node) {
			return { name: xmlValue(node, "Name"), createdAt: xmlValue(node, "CreationDate") };
		}).filter(function (bucket) { return bucket.name; });
	}

	async function listBucket(vault, bucket, prefix, maxKeys, decryptNames) {
		var shouldDecrypt = decryptNames !== false;
		var activeBucket = bucketByID(accountByID(state.activeVaultId), state.activeBucketId);
		var cryptoState = shouldDecrypt ? await bucketCrypto(activeBucket) : null;
		var encryptedPrefix = shouldDecrypt ? await encryptPrefix(cryptoState.pathKey, prefix || "") : (prefix || "");
		var entries = [["delimiter", "/"], ["list-type", "2"], ["max-keys", String(maxKeys || 1000)], ["prefix", encryptedPrefix]];
		var text = await storageText(vault, bucket, entries);
		var xml = new DOMParser().parseFromString(text, "application/xml");
		if (xml.getElementsByTagName("parsererror").length) throw new Error("The S3 endpoint returned invalid XML.");
		var folders = xmlChildren(xml, "CommonPrefixes").map(function (node) {
			return { kind: "folder", encryptedKey: xmlValue(node, "Prefix") };
		});
		var files = xmlChildren(xml, "Contents").map(function (node) {
			return { kind: "file", encryptedKey: xmlValue(node, "Key"), size: Number(xmlValue(node, "Size") || 0), modified: xmlValue(node, "LastModified") };
		});
		if (!shouldDecrypt) {
			return folders.concat(files).map(function (item) {
				return { kind: item.kind, encryptedKey: item.encryptedKey, plaintextPath: item.encryptedKey, name: item.encryptedKey, size: item.size, modified: item.modified };
			});
		}
		var items = [];
		for (var i = 0; i < folders.length; i++) {
			var folderPath = await decryptObjectKey(cryptoState.pathKey, folders[i].encryptedKey.replace(/\/$/, ""));
			items.push({ kind: "folder", encryptedKey: folders[i].encryptedKey, plaintextPath: folderPath + "/", name: folderPath.slice((prefix || "").length).replace(/\/$/, "") });
		}
		for (var j = 0; j < files.length; j++) {
			var filePath = await decryptObjectKey(cryptoState.pathKey, files[j].encryptedKey);
			if (filePath === prefix) continue;
			items.push({ kind: "file", encryptedKey: files[j].encryptedKey, plaintextPath: filePath, name: filePath.slice((prefix || "").length), size: files[j].size, modified: files[j].modified });
		}
		return items.filter(function (item) { return item.name; });
	}

	function formatSize(size) {
		if (!size) return "—";
		var units = ["B", "KB", "MB", "GB", "TB"];
		var index = 0;
		var value = size;
		while (value >= 1024 && index < units.length - 1) { value /= 1024; index++; }
		return (index === 0 ? value : value.toFixed(value >= 10 ? 0 : 1)) + " " + units[index];
	}

	function formatDate(value) {
		if (!value) return "—";
		var date = new Date(value);
		return Number.isNaN(date.getTime()) ? "—" : date.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
	}

	function renderBreadcrumbs() {
		elements.breadcrumbs.replaceChildren();
		var parts = state.filePrefix ? state.filePrefix.split("/").filter(Boolean) : [];
		var rootButton = document.createElement("button");
		rootButton.type = "button";
		rootButton.textContent = "Root";
		rootButton.dataset.prefix = "";
		elements.breadcrumbs.appendChild(rootButton);
		var current = "";
		parts.forEach(function (part) {
			current += part + "/";
			var separator = document.createElement("span");
			separator.textContent = "/";
			separator.setAttribute("aria-hidden", "true");
			elements.breadcrumbs.appendChild(separator);
			var button = document.createElement("button");
			button.type = "button";
			button.textContent = part;
			button.dataset.prefix = current;
			elements.breadcrumbs.appendChild(button);
		});
	}

	function renderFiles(items) {
		elements.fileList.replaceChildren();
		state.visibleItems = [];
		if (!items.length) {
			var empty = document.createElement("p");
			empty.className = "file-list-empty";
			empty.textContent = state.filePrefix ? "This folder is empty." : "This bucket is empty.";
			elements.fileList.appendChild(empty);
			return;
		}
		var heading = document.createElement("div");
		heading.className = "file-row file-row-heading";
		heading.innerHTML = "<span>Name</span><span>Size</span><span>Modified</span>";
		elements.fileList.appendChild(heading);
		items.sort(function (a, b) { return a.kind === b.kind ? a.name.localeCompare(b.name) : (a.kind === "folder" ? -1 : 1); });
		state.visibleItems = items.slice();
		items.forEach(function (item, index) {
			var row = document.createElement("div");
			row.className = "file-row";
			var name = document.createElement("button");
			name.type = "button";
			name.className = "file-name";
			name.textContent = (item.kind === "folder" ? "▰ " : "▱ ") + item.name;
			name.dataset.itemIndex = String(index);
			name.dataset.kind = item.kind;
			var size = document.createElement("span");
			size.textContent = item.kind === "folder" ? "Folder" : formatSize(item.size);
			var date = document.createElement("span");
			date.textContent = formatDate(item.modified);
			row.appendChild(name);
			row.appendChild(size);
			row.appendChild(date);
			elements.fileList.appendChild(row);
		});
	}

	function loadFileList() {
		var vault = accountByID(state.activeVaultId);
		var bucket = bucketByID(vault, state.activeBucketId);
		if (!vault || !bucket) {
			state.permissions.list = false;
			state.permissions.read = false;
			updatePermissionActions();
			elements.fileManagerTitle.textContent = "Select a bucket";
			elements.fileManagerSubtitle.textContent = "Register a bucket to browse its objects.";
			elements.fileList.replaceChildren();
			elements.fileStatus.textContent = "";
			renderBreadcrumbs();
			return;
		}
		var requestID = ++state.fileRequest;
		elements.fileManagerTitle.textContent = bucket.name;
		elements.fileManagerSubtitle.textContent = vault.name + " · " + (activeTransportMode(vault, bucket.name) === "proxy" ? "server proxy, ciphertext only" : "direct browser listing");
		elements.fileStatus.textContent = "Loading objects…";
		state.permissions.list = false;
		state.permissions.read = false;
		updatePermissionActions();
		renderBreadcrumbs();
		listBucket(vault, bucket.name, state.filePrefix, 1000).then(function (items) {
			if (requestID !== state.fileRequest) return;
			renderFiles(items);
			state.permissions.list = true;
			state.permissions.read = true;
			updatePermissionActions();
			elements.fileStatus.textContent = items.length + " item" + (items.length === 1 ? "" : "s") + ". Upload and delete remain disabled until explicitly verified.";
		}).catch(function (error) {
			if (requestID !== state.fileRequest) return;
			elements.fileList.replaceChildren();
			elements.fileStatus.textContent = error.message;
		});
	}

	function updatePermissionActions() {
		elements.uploadFiles.disabled = !state.permissions.upload;
		elements.deleteFiles.disabled = !state.permissions.delete;
		elements.uploadFiles.title = state.permissions.upload ? "Upload is allowed" : "Upload permission has not been verified";
		elements.deleteFiles.title = state.permissions.delete ? "Delete is allowed" : "Delete permission has not been verified";
	}

	function headerValue(headers, names) {
		for (var i = 0; i < names.length; i++) {
			var value = headers.get(names[i]);
			if (value) return value;
		}
		return "";
	}

	async function readObjectEnvelope(vault, bucket, item) {
		var signal = state.viewerAbortController ? state.viewerAbortController.signal : undefined;
		var response = await storageResponse(vault, bucket.name, [], item.encryptedKey, "bytes=0-0", signal);
		if (response.status !== 206) throw new Error("Storage did not honor the metadata byte range.");
		var iv = headerValue(response.headers, ["X-S3Zero-IV", "x-amz-meta-s3zero-iv"]);
		var keyHash = headerValue(response.headers, ["X-S3Zero-KeyHash", "x-amz-meta-s3zero-keyhash"]);
		if (!iv || !keyHash) throw new Error("Object is missing its S3 Zero encryption metadata.");
		var cryptoState = await bucketCrypto(bucket);
		var fileKey = await hkdfBytes(cryptoState.bucketKey, utf8(item.plaintextPath), utf8("s3zero-file-key"));
		var calculatedHash = await sha256(fileKey);
		if (calculatedHash.toLowerCase() !== keyHash.toLowerCase()) throw new Error("Master file does not match this object.");
		if (!/^[0-9a-f]{16}$/i.test(iv)) throw new Error("Object encryption metadata contains an invalid nonce.");
		var nonce = new Uint8Array(iv.match(/.{2}/g).map(function (part) { return parseInt(part, 16); }));
		var contentRange = response.headers.get("Content-Range") || "";
		var rangeMatch = contentRange.match(/^bytes 0-0\/([0-9]+)$/);
		if (!rangeMatch) throw new Error("Storage returned an invalid metadata range.");
		var size = Number(rangeMatch[1]);
		return { fileKey: fileKey, nonce: nonce, size: size, contentType: headerValue(response.headers, ["Content-Type"]) || "application/octet-stream" };
	}

	function assertRangeResponse(response, start, end) {
		if (response.status !== 206) throw new Error("Storage did not honor the requested video range.");
		var contentRange = response.headers.get("Content-Range") || "";
		var match = contentRange.match(/^bytes ([0-9]+)-([0-9]+)\/([0-9]+)$/);
		if (!match || Number(match[1]) !== start || Number(match[2]) !== end) throw new Error("Storage returned an invalid video range.");
	}

	function guessMediaType(path) {
		var lower = path.toLowerCase();
		if (/\.(png|jpe?g|gif|webp|avif|bmp|svg)$/.test(lower)) return "image/" + (lower.endsWith(".jpg") || lower.endsWith(".jpeg") ? "jpeg" : lower.slice(lower.lastIndexOf(".") + 1));
		if (/\.(mp4|m4v|webm|ogv|mov|mkv)$/.test(lower)) return lower.endsWith(".webm") ? "video/webm" : "video/mp4";
		return "application/octet-stream";
	}

	function clearViewer() {
		if (state.viewerAbortController) state.viewerAbortController.abort();
		state.viewerAbortController = null;
		if (state.viewerURL) URL.revokeObjectURL(state.viewerURL);
		state.viewerURL = "";
		if (state.mediaSource) {
			try { state.mediaSource.endOfStream(); } catch (_) {}
		}
		state.mediaSource = null;
		elements.viewerImage.hidden = true;
		elements.viewerVideo.hidden = true;
		elements.viewerMessage.hidden = true;
		elements.viewerImage.removeAttribute("src");
		elements.viewerVideo.removeAttribute("src");
		elements.viewerVideo.load();
	}

	async function openImageViewer(vault, bucket, item, envelope) {
		elements.viewerStatus.textContent = "Fetching encrypted image…";
		var signal = state.viewerAbortController ? state.viewerAbortController.signal : undefined;
		var response = await storageResponse(vault, bucket.name, [], item.encryptedKey, "", signal);
		var encrypted = new Uint8Array(await response.arrayBuffer());
		var plaintext = await aesCTRBytes(envelope.fileKey, envelope.nonce, 0, encrypted);
		var type = envelope.contentType.startsWith("image/") ? envelope.contentType : guessMediaType(item.plaintextPath);
		state.viewerURL = URL.createObjectURL(new Blob([plaintext], { type: type }));
		elements.viewerImage.src = state.viewerURL;
		elements.viewerImage.alt = item.plaintextPath;
		elements.viewerImage.hidden = false;
		elements.viewerStatus.textContent = "Decrypted in this browser. The server received only ciphertext.";
	}

	async function openVideoViewer(vault, bucket, item, envelope) {
		var type = envelope.contentType.startsWith("video/") ? envelope.contentType : guessMediaType(item.plaintextPath);
		if (!window.MediaSource) throw new Error("This browser does not support MediaSource video streaming.");
		if (!MediaSource.isTypeSupported(type)) {
			var codecHint = /^video\/mp4(?:;|$)/i.test(type) && !/;\s*codecs=/i.test(type)
				? " Store MP4s with a Content-Type that includes codecs (for example, video/mp4; codecs=\"avc1.64001F, mp4a.40.2\")."
				: "";
			throw new Error("This browser cannot stream the detected video type through MediaSource: " + type + "." + codecHint);
		}
		var mediaSource = new MediaSource();
		state.mediaSource = mediaSource;
		state.viewerURL = URL.createObjectURL(mediaSource);
		elements.viewerVideo.src = state.viewerURL;
		elements.viewerVideo.hidden = false;
		elements.viewerStatus.textContent = "Streaming encrypted ranges; decrypting chunks in this browser…";
		mediaSource.addEventListener("sourceopen", function () {
			var sourceBuffer;
			try { sourceBuffer = mediaSource.addSourceBuffer(type); } catch (error) {
				elements.viewerStatus.textContent = "The browser rejected this media codec: " + error.message;
				return;
			}
			var offset = 0;
			var chunkSize = 4 * 1024 * 1024;
			var appendNext = function () {
				if (offset >= envelope.size) {
					try { mediaSource.endOfStream(); } catch (_) {}
					elements.viewerStatus.textContent = "Streaming and decryption are client-side.";
					return;
				}
				var start = offset;
				var end = Math.min(envelope.size, start + chunkSize);
				var signal = state.viewerAbortController ? state.viewerAbortController.signal : undefined;
				storageResponse(vault, bucket.name, [], item.encryptedKey, "bytes=" + start + "-" + (end - 1), signal).then(function (response) {
					assertRangeResponse(response, start, end - 1);
					return response.arrayBuffer();
				}).then(function (buffer) {
					return aesCTRBytes(envelope.fileKey, envelope.nonce, start, new Uint8Array(buffer));
				}).then(function (plaintext) {
					sourceBuffer.addEventListener("updateend", appendNext, { once: true });
					sourceBuffer.appendBuffer(plaintext);
					offset = end;
				}).catch(function (error) {
					if (error.name === "AbortError") return;
					elements.viewerStatus.textContent = "Video stream failed: " + error.message;
				});
			};
			appendNext();
		}, { once: true });
	}

	async function openViewer(index) {
		var vault = accountByID(state.activeVaultId);
		var bucket = bucketByID(vault, state.activeBucketId);
		var item = state.visibleItems[index];
		if (!vault || !bucket || !item || item.kind !== "file") return;
		elements.viewerTitle.textContent = item.plaintextPath;
		elements.viewerStatus.textContent = "Validating client-side encryption metadata…";
		elements.viewerMessage.hidden = true;
		clearViewer();
		state.viewerAbortController = new AbortController();
		elements.viewerTitle.textContent = item.plaintextPath;
		elements.viewer.showModal();
		try {
			var envelope = await readObjectEnvelope(vault, bucket, item);
			var type = envelope.contentType.startsWith("image/") || envelope.contentType.startsWith("video/") ? envelope.contentType : guessMediaType(item.plaintextPath);
			if (type.startsWith("image/")) await openImageViewer(vault, bucket, item, envelope);
			else if (type.startsWith("video/")) await openVideoViewer(vault, bucket, item, envelope);
			else {
				elements.viewerStatus.textContent = "This file is encrypted and verified, but no image/video viewer is available for its type.";
				elements.viewerMessage.textContent = "Viewer support is currently limited to images and video.";
				elements.viewerMessage.hidden = false;
			}
		} catch (error) {
			if (error.name === "AbortError") return;
			elements.viewerStatus.textContent = error.message;
			elements.viewerMessage.textContent = "Nothing was decrypted or displayed.";
			elements.viewerMessage.hidden = false;
		}
	}

	function importVaults(event) {
		var file = event.target.files && event.target.files[0];
		event.target.value = "";
		if (!file) return;
		if (file.size > MAX_IMPORT_BYTES) {
			showToast("Import files are limited to 1 MB.", "error");
			return;
		}
		file.text().then(function (text) {
			var parsed;
			try { parsed = JSON.parse(text); } catch (_) { throw new Error("That file is not valid JSON."); }
			var rawVaults = Array.isArray(parsed) ? parsed : parsed && (parsed.vaults || parsed.accounts);
			if (!Array.isArray(rawVaults) || rawVaults.length === 0) throw new Error("No vault records were found.");
			if (rawVaults.length > MAX_IMPORT_VAULTS) throw new Error("Imports are limited to 500 vaults at a time.");
			var valid = [];
			var rejected = 0;
			rawVaults.forEach(function (raw) {
				var vault = normalizeVault(raw, true);
				if (requiredVaultFields(vault).length || !validEndpoint(vault.endpoint)) rejected++;
				else valid.push(vault);
			});
			if (!valid.length) throw new Error("No complete vault records could be imported.");
			return saveVaults(valid).then(function () {
				state.vaults = state.vaults.concat(valid).sort(function (a, b) { return a.createdAt - b.createdAt; });
				state.activeVaultId = valid[0].id;
				state.activeBucketId = valid[0].buckets.length ? valid[0].buckets[0].id : null;
				state.view = "files";
				render();
				loadFileList();
				showToast("Imported " + valid.length + " vault" + (valid.length === 1 ? "" : "s") + (rejected ? "; skipped " + rejected + " incomplete record" + (rejected === 1 ? "" : "s") : "."), "success");
			});
		}).catch(function (error) { showToast(error.message || "Could not import that file.", "error"); });
	}

	function exportVaults() {
		if (!state.vaults.length) {
			showToast("There are no vaults to export yet.", "error");
			return;
		}
		if (!window.confirm("This JSON file contains plaintext S3 credentials and base64 master files. Export it only to a secure location?")) return;
		var payload = { format: "s3zero-vaults", version: 2, exportedAt: new Date().toISOString(), vaults: state.vaults };
		var blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
		var url = URL.createObjectURL(blob);
		var link = document.createElement("a");
		link.href = url;
		link.download = "s3zero-vaults.json";
		document.body.appendChild(link);
		link.click();
		link.remove();
		window.setTimeout(function () { URL.revokeObjectURL(url); }, 0);
		showToast("Vault JSON exported.", "success");
	}

	function toggleSecret(event) {
		var button = event.currentTarget;
		var field = document.getElementById(button.dataset.reveal);
		if (!field) return;
		var visible = field.type === "text";
		field.type = visible ? "password" : "text";
		button.textContent = visible ? "Show" : "Hide";
		button.setAttribute("aria-label", (visible ? "Show " : "Hide ") + secretLabel(field.id));
	}

	elements.addVault.addEventListener("click", startNewVault);
	elements.emptyAddVault.addEventListener("click", startNewVault);
	elements.vaultForm.addEventListener("submit", saveCurrentVault);
	elements.deleteVault.addEventListener("click", deleteActiveVault);
	elements.cancelVault.addEventListener("click", cancelVault);
	elements.testConnection.addEventListener("click", testConnection);
	elements.addBucket.addEventListener("click", startBucketSetup);
	elements.cancelBucket.addEventListener("click", cancelBucket);
	elements.verifyBucket.addEventListener("click", verifyBucket);
	elements.saveBucket.addEventListener("click", saveRegisteredBucket);
	elements.refreshFiles.addEventListener("click", loadFileList);
	elements.editVault.addEventListener("click", editActiveVault);
	elements.openModeDirect.addEventListener("click", function () { setTransportMode("direct", false); });
	elements.openModeProxy.addEventListener("click", function () { setTransportMode("proxy", false); });
	elements.closeViewer.addEventListener("click", function () { elements.viewer.close(); });
	elements.viewer.addEventListener("close", clearViewer);
	elements.importVaults.addEventListener("click", function () { elements.importFile.click(); });
	elements.importFile.addEventListener("change", importVaults);
	elements.exportVaults.addEventListener("click", exportVaults);
	elements.vaultList.addEventListener("click", function (event) {
		var button = event.target.closest("[data-vault-id]");
		if (button) openVault(button.dataset.vaultId);
	});
	elements.bucketList.addEventListener("click", function (event) {
		var button = event.target.closest("[data-bucket-id]");
		if (button) openBucket(button.dataset.bucketId);
	});
	elements.bucketChoice.addEventListener("input", masterChanged);
	elements.bucketChoice.addEventListener("change", masterChanged);
	elements.masterText.addEventListener("input", masterChanged);
	elements.masterFile.addEventListener("change", function () {
		var file = elements.masterFile.files && elements.masterFile.files[0];
		elements.masterFileName.textContent = file ? file.name + " · " + formatSize(file.size) : "No file selected.";
		masterChanged();
	});
	elements.vaultForm.addEventListener("input", function (event) {
		if (event.target === elements.fields.name || event.target === elements.fields.endpoint || event.target === elements.fields.region || event.target === elements.fields.accessKeyId || event.target === elements.fields.secretAccessKey || event.target === elements.fields.sessionToken || event.target === elements.fields.proxyAccessToken || event.target === elements.knownVaultBucket || event.target === elements.fields.pathStyle || Array.prototype.indexOf.call(elements.fields.transportMode, event.target) >= 0) clearConnectionTest();
	});
	elements.fileList.addEventListener("click", function (event) {
		var button = event.target.closest("[data-kind]");
		if (!button) return;
		var item = state.visibleItems[Number(button.dataset.itemIndex)];
		if (!item) return;
		if (button.dataset.kind === "folder") {
			state.filePrefix = item.plaintextPath;
			loadFileList();
		} else {
			openViewer(Number(button.dataset.itemIndex));
		}
	});
		elements.breadcrumbs.addEventListener("click", function (event) {
		var button = event.target.closest("[data-prefix]");
		if (!button) return;
		state.filePrefix = button.dataset.prefix;
		loadFileList();
	});
	Array.prototype.forEach.call(elements.fields.transportMode, function (input) {
		input.addEventListener("change", function () { setTransportMode(input.value, true); });
	});
	root.querySelectorAll("[data-reveal]").forEach(function (button) { button.addEventListener("click", toggleSecret); });

	checkProxyAvailability().then(function () { return openDatabase(); }).then(function (database) {
		state.db = database;
		return readVaults();
	}).then(function (vaults) {
		state.vaults = vaults;
		if (vaults.length) {
			state.activeVaultId = vaults[0].id;
			state.activeBucketId = vaults[0].buckets.length ? vaults[0].buckets[0].id : null;
			state.view = "files";
		}
		render();
		if (state.view === "files") loadFileList();
	}).catch(function (error) {
		showToast(error.message || "Local vault storage is unavailable.", "error");
		render();
	});
})();
