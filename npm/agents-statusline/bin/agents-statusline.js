#!/usr/bin/env node
const { spawnSync } = require("node:child_process");

const pkgFamily = "@morgan.rebrand/agents-statusline-";
const platformPackages = {
	"darwin/arm64": "darwin-arm64",
	"darwin/x64": "darwin-x64",
	"linux/x64": "linux-x64",
	"linux/arm64": "linux-arm64",
	"win32/x64": "win32-x64",
};

function platform() {
	const key = `${process.platform}/${process.arch}`;
	if (platformPackages[key]) return platformPackages[key];
	console.error(
		`agents-statusline: unsupported platform ${key}. Supported: ${Object.keys(platformPackages).join(", ")}.`,
	);
	process.exit(1);
}

function runBinary(bin) {
	const r = spawnSync(bin, process.argv.slice(2), {
		stdio: "inherit",
		shell: false,
	});
	if (r.error) {
		console.error(
			`agents-statusline: could not run ${bin}: ${r.error.message}`,
		);
		process.exit(1);
	}
	process.exit(r.status ?? 1);
}

function main() {
	const bin = process.env.AGENTS_STATUSLINE_BIN;
	if (bin) {
		runBinary(bin);
	}

	const pkg = pkgFamily + platform();
	const binName =
		process.platform === "win32"
			? "agents-statusline.exe"
			: "agents-statusline";
	let resolved;
	try {
		resolved = require.resolve(`${pkg}/bin/${binName}`);
	} catch (e) {
		if (e && e.code === "MODULE_NOT_FOUND") {
			console.error(
				`agents-statusline: optional dependency ${pkg} is missing. Re-run "npm install -g @morgan.rebrand/agents-statusline", or set AGENTS_STATUSLINE_BIN to the absolute path of a agents-statusline binary.`,
			);
			process.exit(1);
		}
		throw e;
	}

	runBinary(resolved);
}

main();
