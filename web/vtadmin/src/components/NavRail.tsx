/**
 * Copyright 2021 The Vitess Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
import * as React from 'react';
import { Link, NavLink } from 'react-router-dom';

import style from './NavRail.module.scss';
import logo from '../img/vitess-icon-color.svg';
import { useClusters, useGates, useKeyspaces, useSchemas, useTablets, useVtctlds, useWorkflows } from '../hooks/api';
import { Icon, Icons } from './Icon';
import { getTableDefinitions } from '../util/tableDefinitions';

export const NavRail = () => {
    const { data: clusters = [] } = useClusters();
    const { data: keyspaces = [] } = useKeyspaces();
    const { data: gates = [] } = useGates();
    const { data: schemas = [] } = useSchemas();
    const { data: tablets = [] } = useTablets();
    const { data: vtctlds = [] } = useVtctlds();
    const { data: workflows = [] } = useWorkflows();

    const tds = React.useMemo(() => getTableDefinitions(schemas), [schemas]);

    return (
        <div className={style.container}>
            <Link className={style.logoContainer} to="/">
                <img alt="Vitess logo" className="h-16 mx-auto" src={logo} height={40}></img>
            </Link>

            <div className={style.navLinks}>
                <ul className={style.navList}>
                    <li>
                        <NavRailLink icon={Icons.chart} text="Dashboard" to="/dashboard" count={0} />
                    </li>
                    <li>
                        <NavRailLink icon={Icons.wrench} text="Workflows" to="/workflows" count={workflows.length} />
                    </li>
                </ul>

                <ul className={style.navList}>
                    <li>
                        {/* FIXME replace this with a C when we have one */}
                        <NavRailLink icon={Icons.keyR} text="Clusters" to="/clusters" count={clusters.length} />
                    </li>
                    <li>
                        <NavRailLink icon={Icons.keyG} text="Gates" to="/gates" count={gates.length} />
                    </li>
                    <li>
                        <NavRailLink icon={Icons.keyK} text="Keyspaces" to="/keyspaces" count={keyspaces.length} />
                    </li>
                    <li>
                        <NavRailLink icon={Icons.keyS} text="Schemas" to="/schemas" count={tds.length} />
                    </li>
                    <li>
                        <NavRailLink icon={Icons.keyT} text="Tablets" to="/tablets" count={tablets.length} />
                    </li>
                    <li>
                        <NavRailLink icon={Icons.keyV} text="vtctlds" to="/vtctlds" count={vtctlds.length} />
                    </li>
                </ul>

                <ul className={style.navList}>
                    <li>
                        <NavRailLink icon={Icons.download} text="Backups" to="/backups" />
                    </li>
                    <li>
                        <NavRailLink icon={Icons.runQuery} text="VTExplain" to="/vtexplain" />
                    </li>
                    <li>
                        <NavRailLink icon={Icons.gear} text="Settings" to="/settings" />
                    </li>
                </ul>
            </div>

            <div className={style.footerContainer}>
                <ul className={style.navList}>
                    <li>
                        <NavRailLink icon={Icons.bug} text="Debug" to="/debug" />
                    </li>
                    <li>
                        <NavRailLink icon={Icons.keyboard} text="Shortcuts" to="/shortcuts" />
                    </li>
                </ul>
            </div>
        </div>
    );
};

const NavRailLink = ({ count, icon, text, to }: { count?: number; icon: Icons; text: string; to: string }) => {
    return (
        <NavLink activeClassName={style.navLinkActive} className={style.navLink} to={to}>
            <Icon className={style.icon} icon={icon} />
            <span>{text}</span>
            {typeof count === 'number' && <div className={style.badge}>{count}</div>}
        </NavLink>
    );
};
