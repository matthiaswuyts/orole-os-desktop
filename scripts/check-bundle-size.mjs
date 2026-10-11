// Fails when the frontend entry chunk (the JS file index.html loads first) is bigger than the budget
// in scripts/bundle-budget.json. Run after `bun run --cwd frontend build`:
//
//   bun scripts/check-bundle-size.mjs            check, exit 1 over budget
//   bun scripts/check-bundle-size.mjs --ratchet  lower the budget to the measured size (never raises it)
//
// The budget is measured raw (uncompressed) bytes, which are deterministic for a given lockfile. When
// a change shrinks the entry chunk, run --ratchet and commit the new budget with it, so the saving
// cannot silently grow back. Raising the budget is a deliberate edit of the JSON file in a PR that
// explains why. See docs/development/testing.md.
import { existsSync, readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const assets = join(root, 'frontend', 'dist', 'assets')
const indexHtml = join(root, 'frontend', 'dist', 'index.html')
const budgetPath = join(root, 'scripts', 'bundle-budget.json')
// Headroom the ratchet leaves above the measured size, rounded up to a whole KiB.
const RATCHET_HEADROOM = 0.01
// A budget this much above the measured size is reported as stale.
const STALE_RATIO = 0.05

const fail = (message) => {
  console.error(`bundle-size: ${message}`)
  process.exit(1)
}

const kib = (bytes) => `${(bytes / 1024).toFixed(1)} KiB`

// The entry chunk is the module script in dist/index.html. Code-split chunks can also be named
// index-<hash>.js, so a bare glob is only the fallback.
function entryChunk() {
  if (!existsSync(assets)) fail(`${assets} does not exist; run \`bun run --cwd frontend build\` first`)
  if (existsSync(indexHtml)) {
    const html = readFileSync(indexHtml, 'utf8')
    const match = html.match(/<script[^>]*type="module"[^>]*src="([^"]+\.js)"/) ?? html.match(/<script[^>]*src="([^"]+\.js)"[^>]*type="module"/)
    if (match) {
      const file = join(root, 'frontend', 'dist', match[1].replace(/^\//, ''))
      if (!existsSync(file)) fail(`index.html loads ${match[1]}, which is not in dist`)
      return file
    }
  }
  const candidates = readdirSync(assets).filter((name) => /^index-.*\.js$/.test(name))
  if (candidates.length === 0) fail(`no index-*.js in ${assets}`)
  return join(assets, candidates.map((name) => ({ name, size: statSync(join(assets, name)).size })).sort((a, b) => b.size - a.size)[0].name)
}

if (!existsSync(budgetPath)) fail(`${budgetPath} is missing`)
const budget = JSON.parse(readFileSync(budgetPath, 'utf8'))
if (!Number.isInteger(budget.entryMaxBytes) || budget.entryMaxBytes <= 0) fail('entryMaxBytes in bundle-budget.json must be a positive integer')

const file = entryChunk()
const bytes = statSync(file).size
const gzip = gzipSync(readFileSync(file)).length
const name = file.slice(assets.length + 1)
console.log(`entry chunk ${name}: ${bytes} bytes (${kib(bytes)}), gzip ${kib(gzip)}; budget ${budget.entryMaxBytes} bytes (${kib(budget.entryMaxBytes)})`)

if (process.argv.includes('--ratchet')) {
  const next = Math.ceil((bytes * (1 + RATCHET_HEADROOM)) / 1024) * 1024
  if (next >= budget.entryMaxBytes) {
    console.log('budget is already at or below the measured size plus headroom; unchanged')
  } else {
    writeFileSync(budgetPath, `${JSON.stringify({ ...budget, entryMaxBytes: next }, null, 2)}\n`)
    console.log(`budget lowered ${budget.entryMaxBytes} -> ${next} bytes; commit scripts/bundle-budget.json`)
  }
  process.exit(0)
}

if (bytes > budget.entryMaxBytes) {
  fail(`entry chunk is ${bytes - budget.entryMaxBytes} bytes over budget. Split the new code behind a dynamic import() or, if the growth is intended, raise entryMaxBytes in scripts/bundle-budget.json and say why in the PR.`)
}
if (bytes < budget.entryMaxBytes * (1 - STALE_RATIO)) {
  console.log(`::notice title=Bundle budget can be lowered::entry chunk is ${kib(budget.entryMaxBytes - bytes)} under budget; run \`bun scripts/check-bundle-size.mjs --ratchet\` and commit scripts/bundle-budget.json`)
}
