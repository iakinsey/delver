import React from 'react';
import { createRoot } from 'react-dom/client';
import './theme.css'
import Grid from './views/grid/grid'
import DashboardList from './views/page/dashboardList'
import Header from './views/header'
import Help from './views/page/help'
import Crawler from './views/page/crawler'
import { Provider } from 'react-redux'
import startJobs from './event/jobs'
import store from './event/store'
import 'react-grid-layout/css/styles.css'
import 'react-resizable/css/styles.css'
import { BrowserRouter as Router, Switch, Route } from "react-router-dom";


startJobs()


createRoot(document.getElementById('root')).render(
    <Provider store={store}>
        <Router>
            <Header />
            <Switch>
                <Route exact path="/crawler"><Crawler /></Route>
                <Route exact path="/">
                    <Grid />
                </Route>
                <Route path="/dashboard/:dashId">
                    <Grid />
                </Route>
                <Route exact path="/dashboards">
                    <DashboardList />
                </Route>
                <Route exact path="/help">
                    <Help />
                </Route>
            </Switch>
        </Router>
    </Provider>
);
