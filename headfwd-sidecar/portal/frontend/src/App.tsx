import { Routes, Route } from "react-router-dom";
import Layout from "./components/Layout";
import Users from "./components/Users";

export default function App() {
  return (
    <Layout>
      <Routes>
        <Route path="*" element={<Users />} />
      </Routes>
    </Layout>
  );
}
