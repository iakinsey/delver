import React from 'react';
import SyntaxHighlighter from 'react-syntax-highlighter';
import { zenburn } from 'react-syntax-highlighter/dist/esm/styles/hljs';
import { BOX_BORDER } from '../../styles/color'


zenburn.hljs.margin = "0"


const FILTER_EXAMPLE = JSON.stringify({
    'fields': [],
    'range': 120,
    'query': {
        'keyword': [],
        'country': [],
        'company': []
    }
}, null, 2)


const QUERY_EXAMPLE = JSON.stringify({
    keyword: ['presidential election', 'post truth'],
    country: ['USA', 'GBR'],
    company: ['NASDAQ:INTC', 'NYSE:WEC', 'AMEX:IAF']
}, null, 2)


const MAP_QUERY_EXAMPLE = JSON.stringify({
    "key":"binary_sentiment_naive_bayes_aggregate",
    "fields":[
        "binary_sentiment_naive_bayes_aggregate",
        "found",
        "countries"
    ]
}, null, 2)


const VIDEO_QUERY_EXAMPLE = JSON.stringify({
    url: "https://www.youtube.com/watch?v=9Auq9mYxFEE"
}, null, 2)


const CHART_QUERY_EXAMPLE = JSON.stringify({
    "fields":[
        "binary_sentiment_naive_bayes_aggregate",
        "found"
    ],
    "key":"binary_sentiment_naive_bayes_aggregate"
}, null, 2)

const EXAMPLE_DASH = JSON.stringify({
    "id": "794ef581-7022-41bd-a50e-308a000f5f8a", 
    "name": "Test Dashboard",
    "description": "Stuff goes here.",
    "user_uid": "a45e4247-ba7a-4e7f-b47d-8ae692bc3d28",
    "value": {
        "cells": [
            {
                "uuid": "ae362fa3-ce92-410c-93f3-6d2dbd4c0e6c",
                "type": "textFeed",
                "attributes": {"x": 11, "y": 0, "w": 12, "h": 9},
                "filter": {}, 
                "title": "Text Stream"
            }, {
                "uuid": "36b59895-5f5c-44a5-9a20-17a3294d0c5a", 
                "title": "Global Sentiment",
                "type": "map",
                "attributes": {"x": 0, "y": 0, "w": 11, "h": 14}, 
                "filter": {
                    "fields": ["binary_sentiment_naive_bayes_aggregate", "found", "countries"],
                    "key": "binary_sentiment_naive_bayes_aggregate", 
                    "title": "Sentiment"
                }, 
            }, {
                "uuid": "3229cd2d-de4e-447a-8bff-0d950544a313", 
                "type": "chart", 
                "title": "Sentiment over time",
                "attributes": {"x": 11, "y": 0, "w": 11, "h": 8}, 
                "filter": {
                    "fields": ["binary_sentiment_naive_bayes_aggregate", "found"], 
                    "key": "binary_sentiment_naive_bayes_aggregate", 
                    "title": "Sentiment"
                }, 
            }
        ]
    } 
}, null, 2)


const FIELDS = JSON.stringify([
    "summary",
    "content",
    "title",
    "url",
    "url_md5",
    "origin_url",
    "type",
    "found",
    "binary_sentiment_naive_bayes_summary",
    "binary_sentiment_naive_bayes_content",
    "binary_sentiment_naive_bayes_title",
    "binary_sentiment_naive_bayes_aggregate",
    "countries",
    "ngrams",
    "corporate"
], null, 2)


function getSyntaxBox(string) {
    return (
        <div style={codeStyle}>
            <SyntaxHighlighter language="json" style={zenburn}>
                {string}
            </SyntaxHighlighter>
        </div>
    ) 
}


function API() {
    return (
        <div>
            <h1>JSON Reference</h1>
            <h2>Schema example</h2>
            Most cells conform to the following schema:
            {getSyntaxBox(FILTER_EXAMPLE)}
            <h2>Fields</h2>
            <div>
                Specifies which fields to surface to the cell. Available fields:
            </div>
            {getSyntaxBox(FIELDS)}
            <h2>Range</h2>
            <div>
                An integer representing the number of days to look back.
            </div>
            <h2>Query</h2>
            <div>
                The query key contains lists of attributes. The entity must
                match all attributes contained in each list.
            </div>
            <h3>Keyword</h3>
            <div>
                The keyword filter is a list of strings that should match the
                entity's text content. This field is akin to full-text search.
            </div>
            <h3>Country</h3>
            <div>
                The country key is a list of
                <a href="https://en.wikipedia.org/wiki/ISO_3166-1_alpha-3#Officially_assigned_code_elements">
                    &nbsp;ISO 3166-1 alpha-3&nbsp;
                </a>
                codes that filter geographical areas.
            </div>
            <h3>Company</h3>
            <div>
                The company key references a stock ticker in the following
                format <strong>&nbsp;[Exchange]:[Ticker]</strong>. Supported
                exchanges are: AMEX, NYSE, NASDAQ.
            </div>
            <h3>Example</h3>
            <div>
                {getSyntaxBox(QUERY_EXAMPLE)}
            </div>
            <h2>Cells</h2>
            <h3>News Articles</h3>
            <h3>Chart</h3>
            <div>
                Graphs a data point over time. A <strong>key</strong> must be
                specified that references a field in the
                <strong>&nbsp;fields</strong> list. The best sentiment key is
                currently <strong>binary_sentiment_naive_bayes_aggregate</strong>.
                {getSyntaxBox(CHART_QUERY_EXAMPLE)}
            </div>
            <h3>Map</h3>
            <div>
                Shows sentiment over a map of the world. Requires the
                <strong>&nbsp;countries</strong> key in the <strong>fields</strong> list.
                A <strong>key</strong> must be specified that references a field in
                the <strong>fields</strong> list. The best sentiment key is
                currently <strong>binary_sentiment_naive_bayes_aggregate</strong>.
                {getSyntaxBox(MAP_QUERY_EXAMPLE)}
            </div>
            <h3>Video</h3>
            <div>
                Plays a video. There is only only one key: <strong>url</strong>.
                {getSyntaxBox(VIDEO_QUERY_EXAMPLE)}
            </div>
        </div>
    )
}


function FAQ() {
    return (
        <div>
            <h1>FAQ</h1>
            <h2>Is there an example dashboard?</h2>
            <div>
                Copy and paste the following JSON into the config field when
                creating a new dashboard:

                {getSyntaxBox(EXAMPLE_DASH)}
            </div>
            <h2>Why are no articles from [source X]?</h2>
            <div>
                Delver has limited article processing capabilities. We are
                currently working on a solution that will dramatically increase
                the number of available articles.
            </div>
            <h2>What is Delver's development status?</h2>
            <div>Delver is pre-alpha software.</div>
            <h2>How do I set the title of a cell?</h2>
            <div>
                Click on the current title on the cell's header, type something
                in the text box, and hit enter.
            </div>
            <h2>How is sentiment classified?</h2>
            <div>
                <a href="https://en.wikipedia.org/wiki/Naive_Bayes_classifier">
                    Naive Bayes.
                </a>
            </div>
            <h2>What browser does Delver work best in?</h2>
            <div><a href="/chad.png">Firefox</a>.</div>
        </div>
    )
}


export default function Help() {
    return (
        <div class="help" style={helpContainer}>
            <API />
            <FAQ />
        </div>
    )
}


const codeStyle = {
    border: "2px dashed",
    borderColor: BOX_BORDER,
    marginTop: "1em",
    marginBottom: "1em"
}

const helpContainer = {
    width: "960px",
    margin: "auto"
}
