import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

// Groups follow the documentation map in docs/README.md. Document IDs keep
// the directory of their file, because the docs plugin reads the whole
// repository.
const sidebars: SidebarsConfig = {
  docs: [
    {type: 'doc', id: 'docs/overview', label: 'Overview'},
    {type: 'doc', id: 'guide', label: 'Product guide'},
    {
      type: 'category',
      label: 'Evaluate',
      collapsed: false,
      items: [
        {type: 'doc', id: 'docs/why-git-byline', label: 'Why git-byline'},
        {type: 'doc', id: 'docs/compatibility', label: 'Compatibility'},
        {type: 'doc', id: 'docs/security-model', label: 'Security model'},
      ],
    },
    {
      type: 'category',
      label: 'Adopt',
      collapsed: false,
      items: [
        {type: 'doc', id: 'docs/install', label: 'Installation'},
        {type: 'doc', id: 'marketplace/harness/agent-templates', label: 'Agent templates'},
        {type: 'doc', id: 'action/github-action', label: 'GitHub Action'},
        {type: 'doc', id: 'docs/architecture', label: 'Architecture'},
        {type: 'doc', id: 'docs/interop', label: 'Interoperability'},
      ],
    },
    {
      type: 'category',
      label: 'Build and operate',
      items: [
        {type: 'doc', id: 'docs/design', label: 'Design'},
        {type: 'doc', id: 'docs/validation', label: 'Validation'},
        {type: 'doc', id: 'docs/releases', label: 'Releases'},
        {type: 'doc', id: 'docs/supply-chain', label: 'Supply chain'},
        {type: 'doc', id: 'docs/incident-response', label: 'Incident response'},
        {type: 'doc', id: 'docs/repository-settings', label: 'Repository settings'},
      ],
    },
    {
      type: 'category',
      label: 'Project',
      items: [
        {type: 'doc', id: 'contributing', label: 'Contributing'},
        {type: 'doc', id: 'security-policy', label: 'Security policy'},
        {type: 'doc', id: 'code-of-conduct', label: 'Code of Conduct'},
        {type: 'doc', id: 'changelog', label: 'Changelog'},
      ],
    },
  ],
};

export default sidebars;
