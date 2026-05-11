/** @type {import('next-sitemap').IConfig} */
module.exports = {
  siteUrl: 'https://scitix.github.io/Agent-Sandbox',
  generateRobotsTxt: true,
  outDir: './out',
  // next-sitemap runs after `next build` and writes directly into the static output
  transform: async (defaultTransform, _path) => {
    // Remove basePath prefix — the site is served under /Agent-Sandbox/,
    // but next-sitemap would otherwise prepend it twice.
    return {
      ...defaultTransform,
    };
  },
  additionalPaths: async () => [
    { loc: '/docs/', priority: 0.8, changefreq: 'weekly' },
    { loc: '/docs/api/', priority: 0.7, changefreq: 'weekly' },
    { loc: '/docs/installation/', priority: 0.7, changefreq: 'monthly' },
    { loc: '/docs/integrations/', priority: 0.6, changefreq: 'monthly' },
    { loc: '/docs/changelog/', priority: 0.5, changefreq: 'monthly' },
  ],
  robotsTxtOptions: {
    policies: [
      { userAgent: '*', allow: '/', disallow: ['/api/'] },
    ],
  },
};