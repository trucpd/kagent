import AgentTemplateList from "@/components/AgentTemplateList";
import { AgentTemplatesProvider } from "@/components/AgentTemplatesProvider";

export default function AgentTemplateListPage() {
  return (
    <AgentTemplatesProvider>
      <AgentTemplateList />
    </AgentTemplatesProvider>
  );
}
