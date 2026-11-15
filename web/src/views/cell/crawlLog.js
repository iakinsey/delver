import React from 'react';
import Connectable from '../connectable';
import {request} from '../../util';
export default class CrawlLog extends Connectable{
 constructor(props){super(props);this.state={entries:[],configOn:false,error:'',connection:'Connecting…',hasData:false};}
 componentDidMount(){this.live=true;this.connect();}
 componentDidUpdate(){}
 componentWillUnmount(){this.live=false;clearTimeout(this.timer);this.abort?.abort();}
 async connect(){clearTimeout(this.timer);this.abort?.abort();this.abort=new AbortController();const result=await request('crawler/log',{}, {signal:this.abort.signal});if(!this.live)return;
 if(result.success){this.setState({entries:result.data||[],connection:'Connected',error:'',hasData:!!result.data?.length,lastUpdate:Date.now()});}else this.setState({error:result.error});this.timer=setTimeout(()=>this.connect(),2000);}
 renderQueryBuilder(){return <p>Recent fetch activity for this crawler process. Keeps the latest 500 events, including failed fetches.</p>;}
 updateConfig(){this.setState({configOn:false});}
 render(){return <article className="widget" data-widget-id={this.uuid}>{this.renderConfig()}<div style={{overflow:'auto'}}>{!this.state.entries.length?<p>No crawl activity yet.</p>:<table><thead><tr><th>Time</th><th>URL</th><th>Result</th></tr></thead><tbody>{this.state.entries.slice().reverse().map(entry=><tr key={entry.id}><td>{new Date(entry.timestamp*1000).toLocaleTimeString()}</td><td style={{overflowWrap:'anywhere'}}>{entry.uri}</td><td>{entry.http_status||entry.state}{entry.error&&` · ${entry.error}`}</td></tr>)}</tbody></table>}</div></article>;}
}
