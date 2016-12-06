import React, {
  Component,
  PropTypes
} from 'react';
import {
  connect
} from 'react-redux';
import NomadLink from '../link';
import Table from '../table';
import MetaDisplay from '../meta';
import ConstraintTable from '../constraint/table';

const jobProps = ['ID', 'Name', 'Region', 'Datacenters', 'Status', 'Priority'];

class JobInfo extends Component {

  render() {
    const tasks = [];
    const job = this.props.job;
    const jobMetaBag = job.Meta || {};

    const jobPodId = () => {
      // for some reason, the job.Meta is undefined when running this part of code.
      // so return empty string if it is undefined to avoid crashing this page.
      console.log(`jobPodId(): Meta is ${this.props.job.Meta}`);
      if (this.props.job.Meta) {
        return this.props.job.Meta["COBALT_SERVICE_NAME"] + "-" + this.props.job.Meta["COBALT_PODID"]
      } else {
        return ""
      }
    }

    const actions = () => {
      return <div>
              <span>
                <a href={this.props.job.JenkinsURL+'/job/Mark-As-Default/parambuild/?PODID='+jobPodId()+'&CLUSTER_IP='+this.props.job.NomadAddr} target="_blank" style={{paddingRight:"25px", margin:"25px"}}>
                   <button type="button" className="btn btn-default">Mark As Default</button>
                </a>
              </span>
              <span>
                <a href={this.props.job.JenkinsURL+'/job/CobaltDestroy/parambuild/?COBALT_ID='+this.props.job.ID+'&CLUSTER_IP='+this.props.job.NomadAddr} target='_blank'>
                  <button type="button" className="btn btn-default">Destroy</button>
                </a>
              </span>
            </div>
    }

    // Build the task groups table
    const taskGroups = job.TaskGroups.map((taskGroup) => {
      taskGroup.Tasks.map((task) => {
        tasks.push(
          <tr key={ task.ID }>
                    <td>
                      <NomadLink jobId={ job.ID } taskGroupId={ taskGroup.ID } >
                        { taskGroup.Name }
                      </NomadLink>
                    </td>
                    <td>
                      <NomadLink jobId={ job.ID } taskGroupId={ taskGroup.ID } taskId={ task.ID } >
                        { task.Name }
                      </NomadLink>
                    </td>
                    <td>{ task.Driver }</td>
                    <td>{ task.Resources.CPU }</td>
                    <td>{ task.Resources.MemoryMB }</td>
                    <td>{ task.Resources.DiskMB }</td>
                    <td><ConstraintTable idPrefix={ task.ID } asTooltip constraints={ task.Constraints } /></td>
                  </tr>
        );
        return null;
      });

      const taskGroupMeta = taskGroup.Meta || {};
      return (
        <tr key={ taskGroup.ID }>
                <td>
                  <NomadLink jobId={ job.ID } taskGroupId={ taskGroup.ID } >
                    { taskGroup.Name }
                  </NomadLink>
                </td>
                <td>{ taskGroup.Count }</td>
                <td>{ taskGroup.Tasks.length }</td>
                <td><MetaDisplay asTooltip metaBag={ taskGroupMeta } /></td>
                <td>{ taskGroup.RestartPolicy.Mode }</td>
                <td><ConstraintTable idPrefix={ taskGroup.ID } asTooltip constraints={ taskGroup.Constraints } /></td>
              </tr>
      );
    });

    return (
      <div className="tab-pane active">
            <div className="row">
              <div className="col-lg-6 col-md-6 col-sm-6 col-sx-6 tab-column">
                <legend>Job Properties</legend>
                <dl className="dl-horizontal">
                  { jobProps.map((jobProp) => {
                      let jobPropValue = this.props.job[jobProp];
                      if (Array.isArray(jobPropValue)) {
                          jobPropValue = jobPropValue.join(', ');
                      }

                      const result = [];
                      result.push(<dt>{ jobProp }</dt>);
                      result.push(<dd>{ jobPropValue }</dd>);

                      return result;
                  }, this)}
                </dl>
              </div>
              <div className="col-lg-6 col-md-6 col-sm-6 col-sx-6 tab-column">
                <legend>Meta Properties</legend>
                <MetaDisplay dtWithClass="wide" metaBag={ jobMetaBag } />
              </div>
            </div>

            <div className="row">
              <div className="col-lg-6 col-md-6 col-sm-12 col-sx-12 tab-column">
                <legend>Constraints</legend>
                <ConstraintTable idPrefix={ this.props.job.ID } constraints={ this.props.job.Constraints } />
              </div>
            </div>

            <div className="row">
                <div className="col-lg-6 col-md-6 col-sm-12 col-sx-12 tab-column">
                  <legend>Actions</legend>
                  { actions() }
              </div>
            </div>


            <div className="row">
              <div className="col-lg-12 col-md-12 col-sm-12 col-sx-12 tab-column">
                <legend>Tasks</legend>
                { (tasks.length > 0) ?
                  <Table
                    classes="table table-hover table-striped"
                    headers={ ['Task Group', 'Name', 'Driver', 'CPU', 'Memory', 'Disk', 'Constraints'] }
                    body={ tasks }
                  />
                  : null
                }
              </div>
            </div>
          </div>
    );
  }
}

JobInfo.defaultProps = {
  job: {
    constraints: [],
  },
  allocations: {},
  evaluations: {},
};


function mapStateToProps({
  job,
  allocations,
  evaluations
}) {
  return {
    job,
    allocations,
    evaluations
  };
}

JobInfo.propTypes = {
  job: PropTypes.object.isRequired,
};

export default connect(mapStateToProps)(JobInfo);
