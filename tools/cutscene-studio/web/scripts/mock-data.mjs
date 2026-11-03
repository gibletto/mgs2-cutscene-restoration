// Copies the generated effect catalog into public/mock/ for the UI's offline mock backend
// and the smoke tests. public/mock/ is gitignored: the catalog is derived data and is never
// committed. Generate it first (see the studio README, "Setup").
import { copyFileSync, existsSync, mkdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const web = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const dataDir = process.env.MGS2CS_DATA_DIR || join(web, '..', 'data')
const src = join(dataDir, 'effects.json')
const dst = join(web, 'public', 'mock', 'effects.json')

if (!existsSync(src)) {
  console.log(`mock-data: ${src} not found -- the mock backend will have no effect catalog`)
  process.exit(0)
}
mkdirSync(dirname(dst), { recursive: true })
copyFileSync(src, dst)
console.log(`mock-data: copied ${src} -> ${dst}`)
