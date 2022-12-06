import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter, Routes, Route, Outlet, Link } from "react-router-dom";

function Home() {
    return <div>Home</div>
}

function About() {
    return <div>About</div>
}

function Layout() {
    return (
        <div>
            <Outlet />
        </div>
    )
}

ReactDOM.createRoot(
  document.getElementById('root') as HTMLElement
).render(
  <React.StrictMode>
    {"index"}
    <BrowserRouter>
        <Routes>
            <Route path="/" element={<Layout />}>
                <Route index element={<Home />} />
                <Route path="about" element={<About />} />
            </Route>
        </Routes>
    </BrowserRouter>
  </React.StrictMode>
);
