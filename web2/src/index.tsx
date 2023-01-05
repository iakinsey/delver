import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter, Routes, Route, Outlet, Link } from "react-router-dom";
import Login from "./views/auth"
import { Provider } from "react-redux"
import { store } from "./store"

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
    <Provider store={store}>
        <BrowserRouter>
            <Routes>
                <Route path="/" element={<Layout />}>
                    <Route index element={<Login />} />
                    <Route path="about" element={<About />} />
                </Route>
            </Routes>
        </BrowserRouter>
    </Provider>
  </React.StrictMode>
);
