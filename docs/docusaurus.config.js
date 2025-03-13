// @ts-check
// `@type` JSDoc annotations allow editor autocompletion and type checking
// (when paired with `@ts-check`).
// There are various equivalent ways to declare your Docusaurus config.
// See: https://docusaurus.io/docs/api/docusaurus-config

import {themes as prismThemes} from 'prism-react-renderer';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)
// Updated to trigger GitHub Actions workflow

/** @type {import('@docusaurus/types').Config} */
const config = {
  // Enable Mermaid diagram support
  markdown: {
    mermaid: true,
  },
  themes: ['@docusaurus/theme-mermaid'],
  title: 'DR-Syncer',
  tagline: 'Kubernetes controller for disaster recovery synchronization',
  favicon: 'assets/logo.ico',

  // Set the production url of your site here
  url: 'https://dr-syncer.io',
  // Set the /<baseUrl>/ pathname under which your site is served
  // For GitHub pages deployment with custom domain, use '/'
  baseUrl: '/',

  // GitHub pages deployment config.
  // If you aren't using GitHub pages, you don't need these.
  organizationName: 'supporttools', // Usually your GitHub org/user name.
  projectName: 'dr-syncer', // Usually your repo name.

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'warn',

  // Even if you don't use internationalization, you can use this field to set
  // useful metadata like html lang. For example, if your site is Chinese, you
  // may want to replace "en" with "zh-Hans".
  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          sidebarPath: './sidebars.js',
          // Please change this to your repo.
          // Remove this to remove the "edit this page" links.
          editUrl:
            'https://github.com/supporttools/dr-syncer/tree/main/docs/',
        },
        // Blog feature removed
        theme: {
          customCss: './src/css/custom.css',
        },
      }),
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      // Social card image for sharing
      image: 'https://cdn.support.tools/dr-syncer/logo_no_background.png',
      navbar: {
        title: 'DR-Syncer',
        logo: {
          alt: 'DR-Syncer Logo',
          src: 'https://cdn.support.tools/dr-syncer/logo.svg',
        },
        items: [
          {
            type: 'docSidebar',
            sidebarId: 'tutorialSidebar',
            position: 'left',
            label: 'Documentation',
          },
          {
            href: 'https://github.com/supporttools/dr-syncer',
            label: 'GitHub',
            position: 'right',
          },
        ],
      },
      footer: {
        style: 'dark',
        links: [
          {
            title: 'Docs',
            items: [
              {
                label: 'Documentation',
                to: '/docs/intro',
              },
            ],
          },
          {
            title: 'More',
            items: [
              {
                label: 'GitHub',
                href: 'https://github.com/supporttools/dr-syncer',
              },
            ],
          },
        ],
        copyright: `Copyright © ${new Date().getFullYear()} SupportTools. Built with Docusaurus.`,
      },
      prism: {
        theme: prismThemes.github,
        darkTheme: prismThemes.dracula,
      },
    }),
};

export default config;
