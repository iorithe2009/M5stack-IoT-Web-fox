import { BrowserRouter, Routes, Route } from "react-router-dom";
import DeviceList from "./pages/DeviceList";
import DeviceDetail from "./pages/DeviceDetail";

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<DeviceList />} />
        <Route path="/devices/:deviceKey" element={<DeviceDetail />} />
      </Routes>
    </BrowserRouter>
  );
}

export default App;
