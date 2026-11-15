import React from "react";
import { createRoot } from "react-dom/client";
import "./theme.css";
import Header from "./views/header";
import Crawler from "./views/page/crawler";
import { WorkspaceLanding } from "./views/page/workspace";
import {
  BrowserRouter as Router,
  Switch,
  Route,
  Redirect,
} from "react-router-dom";

createRoot(document.getElementById("root")).render(
  <Router>
    <Header />
    <Switch>
      <Route exact path={["/", "/crawler", "/crawls/:crawlId"]}>
        <WorkspaceLanding fallback={<Crawler />} />
      </Route>
      <Route>
        <Redirect to="/" />
      </Route>
    </Switch>
  </Router>,
);
