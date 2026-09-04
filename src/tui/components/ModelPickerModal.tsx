import React from "react";
import { Box, Text } from "ink";
import SelectInput from "ink-select-input";
import type { ModelInfo } from "../../core/types";

export interface ModelPickerModalProps {
  models: ModelInfo[];
  currentModel: string;
  onSelect: (modelId: string) => void;
}

export const ModelPickerModal: React.FC<ModelPickerModalProps> = ({
  models,
  currentModel,
  onSelect,
}) => {
  const items = models.map(m => {
    const isCurrent = m.id === currentModel || `${m.provider}/${m.id}` === currentModel;
    const badge = m.isFree ? "[FREE]" : "[PAID]";
    const currentTag = isCurrent ? " (active)" : "";
    return {
      label: `${m.displayName} (${m.provider}) ${badge}${currentTag}`,
      value: m.id,
    };
  });

  return (
    <Box
      borderStyle="round"
      borderColor="cyan"
      flexDirection="column"
      paddingX={1}
      marginY={1}
    >
      <Box justifyContent="space-between" marginBottom={1}>
        <Text bold color="cyan">🤖 SWITCH AI MODEL</Text>
        <Text color="gray">[^P / Esc to Close]</Text>
      </Box>
      {items.length === 0 ? (
        <Text color="gray">Discovering models...</Text>
      ) : (
        <SelectInput
          items={items}
          onSelect={item => onSelect(item.value)}
        />
      )}
    </Box>
  );
};
