import { favicons } from 'favicons'
import fs from 'node:fs/promises'
import path from 'node:path'

const root = path.resolve(process.cwd())
const source = path.join(root, 'src', 'assets', 'logo.svg')
const outputDir = path.join(root, 'public')

const response = await favicons(source, {
  path: '/',
  appName: 'Healthmon',
  appShortName: 'Healthmon',
  appDescription: 'Container health monitor',
  developerName: 'healthmon',
  developerURL: 'https://github.com/nikarh/healthmon',
  background: '#111827',
  theme_color: '#111827',
  icons: {
    android: true,
    appleIcon: true,
    appleStartup: false,
    favicons: true,
    windows: false,
    yandex: false,
  },
})

await fs.mkdir(outputDir, { recursive: true })

await Promise.all(
  response.images.map(async (image) => {
    const filePath = path.join(outputDir, image.name)
    await fs.writeFile(filePath, image.contents)
  }),
)

await Promise.all(
  response.files.map(async (file) => {
    const filePath = path.join(outputDir, file.name)
    await fs.writeFile(filePath, file.contents)
  }),
)

const svgTarget = path.join(outputDir, 'favicon.svg')
await fs.copyFile(source, svgTarget)

const manifestTarget = path.join(outputDir, 'site.webmanifest')
try {
  await fs.access(manifestTarget)
} catch {
  const manifest = {
    name: 'Healthmon',
    short_name: 'Healthmon',
    icons: [
      {
        src: '/android-chrome-192x192.png',
        sizes: '192x192',
        type: 'image/png',
      },
      {
        src: '/android-chrome-512x512.png',
        sizes: '512x512',
        type: 'image/png',
      },
    ],
    theme_color: '#111827',
    background_color: '#111827',
    display: 'standalone',
  }
  await fs.writeFile(manifestTarget, JSON.stringify(manifest, null, 2))
}

console.log('Favicons generated in public/')
