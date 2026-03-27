(() => {
  const radioButtonLabels = ["on", "off"];
  const sliderDefaults = ["min", "trans 1", "trans 2", "trans 3", "trans 4", "max"];
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
    let sliderDraft = Array.from({ length: sliderDefaults.length }, (_, i) => stateLabels[i]?.value ?? sliderDefaults[i]);
    let radioDraft = stateLabels.map((input, i) => input.value || radioButtonDefaultLabel(i));

    if (!controlType || !numStates || stateLabels.length === 0) {
      continue;
    }

    const enabledRadioStates = () => Number.parseInt(numStates.value, 10) || 0;

    const snapshotDrafts = () => {
      if (lastControlType === "sliders") {
        sliderDraft = Array.from({ length: sliderDefaults.length }, (_, i) => stateLabels[i]?.value ?? "");
        return;
      }
      const enabledStates = enabledRadioStates();
      radioDraft = stateLabels.map((input, i) => (i < enabledStates ? input.value : ""));
    };

    const renderSliders = () => {
      numStates.value = String(sliderDefaults.length);
      numStates.readOnly = true;
      for (let i = 0; i < stateLabels.length; i++) {
        const input = stateLabels[i];
        const field = input.closest(".state-field");
        if (i < sliderDefaults.length) {
          input.value = sliderDraft[i] !== "" ? sliderDraft[i] : sliderDefaults[i];
          input.disabled = false;
          field?.classList.remove("state-field-disabled");
        } else {
          input.value = "";
          input.disabled = true;
          field?.classList.add("state-field-disabled");
        }
      }
    };

    const renderRadioButtons = () => {
      numStates.readOnly = false;
      if (lastControlType === "sliders") {
        numStates.value = "2";
      }
      const enabledStates = enabledRadioStates();
      for (let i = 0; i < stateLabels.length; i++) {
        const input = stateLabels[i];
        const enabled = i < enabledStates;
        if (enabled) {
          input.value = radioDraft[i] !== "" ? radioDraft[i] : radioButtonDefaultLabel(i);
        } else {
          input.value = "";
        }
        input.disabled = !enabled;
        input.closest(".state-field")?.classList.toggle("state-field-disabled", !enabled);
      }
    };

    const syncSliderState = () => {
      snapshotDrafts();
      if (controlType.value === "sliders") {
        renderSliders();
      } else {
        renderRadioButtons();
      }
      lastControlType = controlType.value;
    };

    controlType.addEventListener("change", syncSliderState);
    numStates.addEventListener("input", syncSliderState);
    syncSliderState();
  }
})();

// Auto-submit selector bar forms when any dropdown changes.
document.querySelectorAll(".selector-bar select").forEach(function(sel) {
  sel.addEventListener("change", function() {
    this.closest("form").submit();
  });
});
