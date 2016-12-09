import React from 'react';

const summaryLabels = ['Starting', 'Running', 'Queued', 'Complete', 'Failed', 'Lost'];

export function getJobStatisticsHeader() {
    const output = [];

    summaryLabels.forEach((key) => {
        output.push(<th width="75" key={ `statistics-header-for-${key}` }>{ key }</th>);
    });

    return output;
}

export function getJobStatisticsRow(job) {
    const counter = {
        Queued: 0,
        Complete: 0,
        Failed: 0,
        Running: 0,
        Starting: 0,
        Lost: 0,
    };

    if (job.JobSummary !== null) {
        const summary = job.JobSummary.Summary;
        Object.keys(summary).forEach((taskGroupID) => {
            counter.Queued += summary[taskGroupID].Queued;
            counter.Complete += summary[taskGroupID].Complete;
            counter.Failed += summary[taskGroupID].Failed;
            counter.Running += summary[taskGroupID].Running;
            counter.Starting += summary[taskGroupID].Starting;
            counter.Lost += summary[taskGroupID].Lost;
        });
    } else {
        Object.keys(counter).forEach(key => (counter[key] = 'N/A'));
    }

    const output = [];
    summaryLabels.forEach((key) => {
        output.push(<td key={ `${job.ID}-${key}` }>{counter[key]}</td>);
    });

    return output;
}
