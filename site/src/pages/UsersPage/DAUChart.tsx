import { WorkspaceSection } from "components/WorkspaceSection/WorkspaceSection"
import { FC } from "react"

import moment from "moment"
import { Line } from "react-chartjs-2"

import * as TypesGen from "../../api/typesGenerated"

export interface DAUChartProps {
  userMetricsData: TypesGen.GetDAUsResponse
}

import {
  CategoryScale,
  Chart as ChartJS,
  ChartOptions,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Title,
  Tooltip,
} from "chart.js"

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip, Legend)

export const DAUChart: FC<DAUChartProps> = ({ userMetricsData }) => {
  const labels = userMetricsData.entries.map((val) => {
    return moment(val.date).format("l")
  })

  const data = userMetricsData.entries.map((val) => {
    return val.daus
  })

  const options = {
    responsive: true,
    plugins: {
      legend: {
        display: false,
      },
    },
    scales: {
      y: {
        min: 0,
      },
      x: {},
    },
    aspectRatio: 6 / 1,
  } as ChartOptions

  return (
    <>
      {/* <p>{JSON.stringify(chartData)}</p> */}

      <WorkspaceSection title="Daily Active Users">
        <Line
          data={{
            labels: labels,
            datasets: [
              {
                data: data,
              },
            ],
          }}
          options={options as any}
          height={400}
        />
      </WorkspaceSection>
    </>
  )
}
