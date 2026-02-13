"use client";

import dynamic from "next/dynamic";

// Dynamically import the existing Vite/React App with SSR disabled
// because it uses BrowserRouter which requires the browser environment
const App = dynamic(() => import("../ui/src/App"), { ssr: false });

export default function ClientApp() {
  return <App />;
}
