import Layout from '@theme/Layout';
import AgentMarquee from '@site/src/components/landing/AgentMarquee';
import CallToAction from '@site/src/components/landing/CallToAction';
import Dashboard from '@site/src/components/landing/Dashboard';
import Faq from '@site/src/components/landing/Faq';
import Hero from '@site/src/components/landing/Hero';
import Install from '@site/src/components/landing/Install';
import Problem from '@site/src/components/landing/Problem';
import Reasons from '@site/src/components/landing/Reasons';
import Recordings from '@site/src/components/landing/Recordings';
import Workflows from '@site/src/components/landing/Workflows';

export default function Home() {
  return (
    <Layout
      title="Line-level AI code attribution for Git"
      description="git-byline records which person, which agent, and which model wrote each line of code, keeps it in Git notes, and runs without cloud, account, or telemetry.">
      <Hero />
      <main>
        <AgentMarquee />
        <Problem />
        <Dashboard />
        <Recordings />
        <Install />
        <Workflows />
        <Reasons />
        <Faq />
        <CallToAction />
      </main>
    </Layout>
  );
}
