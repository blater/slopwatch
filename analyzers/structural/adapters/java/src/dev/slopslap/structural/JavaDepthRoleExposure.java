package dev.slopslap.structural;

import javax.lang.model.element.*;
import java.util.*;

final class JavaDepthRoleExposure {
    private final JavaDepthRoleReachability reachability;
    private final JavaDepthRolePublicSurface publicSurface;

    JavaDepthRoleExposure(JavaDepthRoles owner) {
        reachability = new JavaDepthRoleReachability(owner);
        publicSurface = new JavaDepthRolePublicSurface(owner);
    }

    List<String> publicRoutes(TypeElement implementation, List<TypeElement> contracts,
                              Set<ExecutableElement> exposedMethods) {
        return publicSurface.routes(implementation, contracts, exposedMethods);
    }

    Set<ExecutableElement> candidateExposureMethods(TypeElement implementation,
                                                    List<TypeElement> contracts) {
        return reachability.candidateExposureMethods(implementation, contracts);
    }
}
