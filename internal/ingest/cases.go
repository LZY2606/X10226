package ingest

// Cases are copy-pasteable, domain-specific TGA submissions (not generic
// tables). They are deterministic: a fixed seed and a declared virtual-clock
// origin make every replay byte-identical.

const CaseSamplesV1 = `time,temperature,mass
0.0 min,30.0 C,10.500 mg
0.5 min,35.0 C,10.498 mg
1.0 min,40.0 C,10.495 mg
1.5 min,50.0 C,10.490 mg
2.0 min,65.0 C,10.460 mg
2.5 min,85.0 C,10.300 mg
3.0 min,105.0 C,10.050 mg
3.5 min,120.0 C,9.900 mg
4.0 min,135.0 C,9.820 mg
4.5 min,150.0 C,9.790 mg
5.0 min,165.0 C,9.780 mg
5.5 min,185.0 C,9.800 mg
6.0 min,210.0 C,9.770 mg
6.5 min,240.0 C,9.500 mg
7.0 min,265.0 C,9.000 mg
7.5 min,285.0 C,8.400 mg
8.0 min,300.0 C,8.050 mg
8.5 min,315.0 C,7.900 mg
9.0 min,330.0 C,7.860 mg
9.5 min,345.0 C,7.850 mg
10.0 min,360.0 C,7.852 mg
10.5 min,380.0 C,7.851 mg
11.0 min,405.0 C,7.850 mg
11.5 min,430.0 C,7.848 mg
12.0 min,460.0 C,7.846 mg
12.5 min,490.0 C,7.845 mg
13.0 min,520.0 C,7.844 mg
13.5 min,555.0 C,7.843 mg
14.0 min,590.0 C,7.842 mg
14.5 min,625.0 C,7.841 mg
15.0 min,660.0 C,7.840 mg
`

const CaseDriftV1 = `time,offset,note
0.0 min,0.000 mg,baseline zero
7.5 min,0.030 mg,slow positive drift
15.0 min,0.060 mg,end-of-run drift
`

const CaseProgramV1 = `start_time,end_time,start_temperature,end_temperature,kind,label
0.0 min,2.0 min,30 C,65 C,ramp,initial heat
2.0 min,5.0 min,65 C,165 C,ramp,moisture ramp
5.0 min,6.0 min,165 C,210 C,ramp,shoulder
6.0 min,9.5 min,210 C,345 C,ramp,main devolatilization
9.5 min,11.0 min,345 C,405 C,isothermal,hold N2
11.0 min,15.0 min,405 C,660 C,ramp,high temperature
`

const CaseGasV1 = `time,gas,action,flow,note
0.0 min,N2,open,50 sccm,start nitrogen
9.0 min,air,switch,50 sccm,valve switch to air
`

const CaseBlankV1 = `time,temperature,mass
0.0 min,30 C,0.000 mg
5.0 min,165 C,0.004 mg
10.0 min,360 C,0.010 mg
15.0 min,660 C,0.018 mg
`

const CaseInitMassV1 = `time,mass,balance_id,note
-5.0 min,10.500 mg,BAL-02,cold weighing
`

// ---- revisions / counter-examples -------------------------------------------

// SamplesV2 introduces an explicit sampling gap (rows 8.5..10.0 missing are
// represented by skipping furnace time), still no interpolation is allowed.
const CaseSamplesV2Gap = `time,temperature,mass
0.0 min,30.0 C,10.500 mg
0.5 min,35.0 C,10.498 mg
1.0 min,40.0 C,10.495 mg
1.5 min,50.0 C,10.490 mg
2.0 min,65.0 C,10.460 mg
2.5 min,85.0 C,10.300 mg
3.0 min,105.0 C,10.050 mg
3.5 min,120.0 C,9.900 mg
4.0 min,135.0 C,9.820 mg
4.5 min,150.0 C,9.790 mg
5.0 min,165.0 C,9.780 mg
5.5 min,185.0 C,9.800 mg
6.0 min,210.0 C,9.770 mg
6.5 min,240.0 C,9.500 mg
7.0 min,265.0 C,9.000 mg
7.5 min,285.0 C,8.400 mg
10.5 min,380.0 C,8.100 mg
11.0 min,405.0 C,8.050 mg
11.5 min,430.0 C,8.048 mg
12.0 min,460.0 C,8.046 mg
12.5 min,490.0 C,8.045 mg
13.0 min,520.0 C,8.044 mg
13.5 min,555.0 C,8.043 mg
14.0 min,590.0 C,8.042 mg
14.5 min,625.0 C,8.041 mg
15.0 min,660.0 C,8.040 mg
`

// InitMassV2 revises only the weighed-in mass (local dependency change).
const CaseInitMassV2 = `time,mass,balance_id,note
-5.0 min,10.500 mg,BAL-02,cold weighing
-4.5 min,10.620 mg,BAL-02,revised weighing after buoyancy check
`

// DriftV2 strengthens baseline drift late in the run.
const CaseDriftV2 = `time,offset,note
0.0 min,0.000 mg,baseline zero
7.5 min,0.040 mg,drift
15.0 min,0.120 mg,strong end drift
`

// GasV2 moves the valve switch earlier; transport delay must still place the
// reaction window after the valve time.
const CaseGasV2 = `time,gas,action,flow,note
0.0 min,N2,open,50 sccm,purge
8.0 min,air,switch,50 sccm,earlier valve switch
`

// HalfFailLayer is a deliberately malformed gas submission: bad action token.
const CaseGasBad = `time,gas,action,flow,note
0.0 min,N2,open,50 sccm,purge
9.0 min,air,purge_instead,50 sccm,bad action
`

// SamplesBadHeader demonstrates an explicit format error (wrong header).
const CaseSamplesBadHeader = `t,temp,m
0,30,10.5
`

// SamplesBadRows has two good rows and one unparsable row: row diagnostics but
// the layer commits (partial import at the row level).
const CaseSamplesBadRows = `time,temperature,mass
0.0 min,30 C,10.500 mg
1.0 min,not-a-temp,10.490 mg
2.0 min,65 C,10.460 mg
`
