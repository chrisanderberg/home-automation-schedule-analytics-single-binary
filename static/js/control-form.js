(() => {
  const radioButtonLabels = ["on", "off"];
  const sliderLabels = ["min", "trans 1", "trans 2", "trans 3", "trans 4", "max"];
  const radioButtonDefaultLabel = (index) => {
    if (index < radioButtonLabels.length) {
      return radioButtonLabels[index];
    }
    return `state ${index + 1}`;
  };

  const forms = document.querySelectorAll(".control-config-form");
  for (const form of forms) {
    const controlType = form.querySelector('select[name="controlType"]');
    const numStates = form.querySelector('input[name="numStates"]');
    const stateLabels = Array.from(form.querySelectorAll('input[name="stateLabel"]'));
    let lastControlType = controlType ? controlType.value : "";

    if (!controlType || !numStates || stateLabels.length === 0) {
      continue;
    }

    const syncSliderState = () => {
      if (controlType.value === "sliders") {
        numStates.value = String(sliderLabels.length);
        numStates.readOnly = true;
        for (let i = 0; i < stateLabels.length; i++) {
          const input = stateLabels[i];
          const field = input.closest(".state-field");
          if (i < sliderLabels.length) {
            input.disabled = false;
            field?.classList.remove("state-field-disabled");
            if (input.value.trim() === "") {
              input.value = sliderLabels[i];
            }
          } else {
            input.value = "";
            input.disabled = true;
            field?.classList.add("state-field-disabled");
          }
        }
        lastControlType = controlType.value;
        return;
      }

      numStates.readOnly = false;
      if (lastControlType === "sliders") {
        for (let i = 0; i < sliderLabels.length && i < stateLabels.length; i++) {
          sliderLabels[i] = stateLabels[i].value;
        }
      }
      if (lastControlType === "sliders" && controlType.value === "radio buttons") {
        numStates.value = "2";
      }
      const enabledStates = Number.parseInt(numStates.value, 10) || 0;
      for (let i = 0; i < stateLabels.length; i++) {
        if (i >= enabledStates) {
          stateLabels[i].value = "";
        }
        stateLabels[i].disabled = i >= enabledStates;
        stateLabels[i].closest(".state-field")?.classList.toggle("state-field-disabled", i >= enabledStates);
      }
      if (controlType.value === "radio buttons" && enabledStates === radioButtonLabels.length) {
        for (let i = 0; i < radioButtonLabels.length; i++) {
          if (stateLabels[i].value.trim() === "") {
            stateLabels[i].value = radioButtonDefaultLabel(i);
          }
        }
      }
      if (controlType.value === "radio buttons" && enabledStates > 0) {
        for (let i = 0; i < enabledStates; i++) {
          if (stateLabels[i].value.trim() === "") {
            stateLabels[i].value = radioButtonDefaultLabel(i);
          }
        }
      }

      lastControlType = controlType.value;
    };

    controlType.addEventListener("change", syncSliderState);
    numStates.addEventListener("input", syncSliderState);
    syncSliderState();
  }
})();
