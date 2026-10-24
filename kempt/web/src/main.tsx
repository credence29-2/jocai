import { createRoot } from "react-dom/client";
import { App } from "./ui/App";
import "./styles.css";
import { get } from "./store";
import { voice, useVoice } from "./voice";

// Handy from the browser console, and used by the end-to-end test.
(window as any).kempt = { voice, voiceState: useVoice.getState, state: get };

createRoot(document.getElementById("root")!).render(<App />);
