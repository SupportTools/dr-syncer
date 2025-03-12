// @ts-check

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

/**
 * Creating a sidebar enables you to:
 - create an ordered group of docs
 - render a sidebar for each doc of that group
 - provide next/previous navigation

 The sidebars can be generated from the filesystem, or explicitly defined here.

 Create as many sidebars as you want.

 @type {import('@docusaurus/plugin-content-docs').SidebarsConfig}
 */
const sidebars = {
  // Manually define sidebar for DR-Syncer documentation
  tutorialSidebar: [
    'intro',
    {
      type: 'category',
      label: 'Overview',
      items: ['overview'],
    },
    {
      type: 'category',
      label: 'Features',
      items: ['features'],
    },
    {
      type: 'category',
      label: 'Installation & Configuration',
      items: ['installation'],
    },
    {
      type: 'category',
      label: 'Examples',
      items: ['examples'],
    },
    {
      type: 'category',
      label: 'Development',
      items: ['development'],
    },
    {
      type: 'category',
      label: 'Troubleshooting',
      items: ['troubleshooting'],
    },
  ],
};

export default sidebars;
