import React from 'react';
import ReactDOM from 'react-dom';
import Grid from './views/grid/grid'
import Login from './views/page/login'
import DashboardList from './views/page/dashboardList'
import Header from './views/header'
import ChangePassword from './views/page/changePassword'
import Register from './views/page/register'
import Logout from './views/page/logout'
import Help from './views/page/help'
import { Provider } from 'react-redux'
import startJobs from './event/jobs'
import store from './event/store'
import 'react-grid-layout/css/styles.css'
import 'react-resizable/css/styles.css'
import { BrowserRouter as Router, Switch, Route } from "react-router-dom";


startJobs()


ReactDOM.render(
    <Provider store={store}>
        <Router>
            <Header />
            <Switch>
                <Route exact path="/">
                    <Grid />
                </Route>
                <Route path="/dashboard/:dashId">
                    <Grid />
                </Route>
                <Route exact path="/login">
                    <Login />
                </Route>
                <Route exact path="/logout">
                    <Logout/>
                </Route>
                <Route exact path="/register">
                    <Register />
                </Route>
                <Route exact path="/dashboards">
                    <DashboardList />
                </Route>
                <Route exact path="/changePassword">
                    <ChangePassword />
                </Route>
                <Route exact path="/help">
                    <Help />
                </Route>
            </Switch>
        </Router>
    </Provider>,
    document.getElementById('root')
);
