import clsx from 'clsx';
import Heading from '@theme/Heading';
import styles from './styles.module.css';

const FeatureList = [
  {
    title: 'Automated Synchronization',
    imageUrl: 'https://cdn.support.tools/dr-syncer/automated_sync.png',
    description: (
      <>
        Schedule and automate resource synchronization between Kubernetes clusters.
        Set up DR environments that stay in sync with your production resources.
      </>
    ),
  },
  {
    title: 'Flexible Configuration',
    imageUrl: 'https://cdn.support.tools/dr-syncer/flexible_config.png',
    description: (
      <>
        Fine-grained control over what gets synchronized. Filter resources by type,
        use exclusion lists, customize deployments, and map namespaces with ease.
      </>
    ),
  },
  {
    title: 'Operational Efficiency',
    imageUrl: 'https://cdn.support.tools/dr-syncer/operational_efficiency.png',
    description: (
      <>
        Minimize manual intervention in DR setup and maintenance. Built-in monitoring,
        health checks, and clear status reporting for your multi-cluster environment.
      </>
    ),
  },
];

function Feature({imageUrl, title, description}) {
  return (
    <div className={clsx('col col--4')}>
      <div className="text--center">
        <img className={styles.featureSvg} src={imageUrl} alt={title} />
      </div>
      <div className="text--center padding-horiz--md">
        <Heading as="h3">{title}</Heading>
        <p>{description}</p>
      </div>
    </div>
  );
}

export default function HomepageFeatures() {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {FeatureList.map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}
